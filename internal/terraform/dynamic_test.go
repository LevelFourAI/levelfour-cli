package terraform

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/zclconf/go-cty/cty"
)

func onlyResource(t *testing.T, content string) map[string]interface{} {
	t.Helper()
	snapshots, _ := parseHCLContent([]byte(content), "test.tf", nil, 0, nil, ParseOptions{}, nil)
	if len(snapshots) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(snapshots))
	}
	return snapshots[0].Attributes
}

func TestDynamicBlock_GeneratesOneBlockPerElement(t *testing.T) {
	attrs := onlyResource(t, `
resource "aws_instance" "web" {
  dynamic "ebs_block_device" {
    for_each = [{ size = 10 }, { size = 20, type = "gp2" }]
    content {
      volume_size = ebs_block_device.value.size
      volume_type = try(ebs_block_device.value.type, "gp3")
      position    = ebs_block_device.key
    }
  }
}
`)

	want := []interface{}{
		map[string]interface{}{"volume_size": float64(10), "volume_type": "gp3", "position": float64(0)},
		map[string]interface{}{"volume_size": float64(20), "volume_type": "gp2", "position": float64(1)},
	}
	if !reflect.DeepEqual(attrs["ebs_block_device"], want) {
		t.Errorf("ebs_block_device = %v, want %v", attrs["ebs_block_device"], want)
	}
	if _, ok := attrs["dynamic"]; ok {
		t.Error("a dynamic block must not be kept under its own keyword")
	}
}

func TestDynamicBlock_UsesANamedIteratorAndLookup(t *testing.T) {
	attrs := onlyResource(t, `
resource "aws_instance" "web" {
  dynamic "root_block_device" {
    for_each = { root = { volume_type = "gp2" } }
    iterator = disk
    content {
      volume_type = lookup(disk.value, "volume_type", "gp3")
      volume_size = lookup(disk.value, "volume_size", 8)
      device      = disk.key
    }
  }
}
`)

	want := []interface{}{map[string]interface{}{"volume_type": "gp2", "volume_size": float64(8), "device": "root"}}
	if !reflect.DeepEqual(attrs["root_block_device"], want) {
		t.Errorf("root_block_device = %v, want %v", attrs["root_block_device"], want)
	}
}

func TestDynamicBlock_GeneratesNothingItCannotEvaluate(t *testing.T) {
	cases := map[string]string{
		"unknown collection": `for_each = var.disks
    content { volume_size = 10 }`,
		"not a collection": `for_each = "disks"
    content { volume_size = 10 }`,
		"null collection": `for_each = null
    content { volume_size = 10 }`,
		"no for_each": `content { volume_size = 10 }`,
		"no content":  `for_each = [1]`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			attrs := onlyResource(t, "resource \"aws_instance\" \"web\" {\n  dynamic \"ebs_block_device\" {\n    "+body+"\n  }\n}\n")
			if _, ok := attrs["ebs_block_device"]; ok {
				t.Errorf("expected no ebs_block_device, got %v", attrs["ebs_block_device"])
			}
			if _, ok := attrs["dynamic"]; ok {
				t.Error("a dynamic block must not be kept under its own keyword")
			}
		})
	}
}

func TestDynamicBlock_ReadsAModuleInput(t *testing.T) {
	parentDir := t.TempDir()
	moduleDir := filepath.Join(parentDir, "instance")
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTF(t, moduleDir, "main.tf", `
variable "root_block_device" {
  default = []
}

resource "aws_instance" "this" {
  instance_type = "m4.xlarge"

  dynamic "root_block_device" {
    for_each = var.root_block_device
    content {
      volume_type = try(root_block_device.value.volume_type, null)
      volume_size = try(root_block_device.value.volume_size, null)
    }
  }
}
`)
	writeTF(t, parentDir, "main.tf", `
module "backend" {
  source = "./instance"
  root_block_device = [{ volume_type = "gp2", volume_size = 80 }]
}
`)

	content, _ := os.ReadFile(filepath.Join(parentDir, "main.tf"))
	snapshots, _ := parseHCLContent(content, filepath.Join(parentDir, "main.tf"), make(map[string]bool), 0, nil, ParseOptions{}, nil)

	want := []interface{}{map[string]interface{}{"volume_type": "gp2", "volume_size": float64(80)}}
	for _, s := range snapshots {
		if s.Type == "aws_instance" && s.Name == "this" {
			if !reflect.DeepEqual(s.Attributes["root_block_device"], want) {
				t.Errorf("root_block_device = %v, want %v", s.Attributes["root_block_device"], want)
			}
			return
		}
	}
	t.Fatal("expected aws_instance.this from the module")
}

func TestModuleInputs_AnInputThatDidNotResolveKeepsTheDefault(t *testing.T) {
	parentDir := t.TempDir()
	moduleDir := filepath.Join(parentDir, "db")
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTF(t, moduleDir, "main.tf", `
variable "size" {
  default = "small"
}

variable "disks" {
  default = []
}

resource "aws_instance" "this" {
  instance_type = var.size == "small" ? "t3.micro" : "m5.large"

  dynamic "ebs_block_device" {
    for_each = var.disks
    content {
      volume_type = ebs_block_device.value.type != null ? ebs_block_device.value.type : "gp3"
    }
  }
}
`)
	writeTF(t, parentDir, "main.tf", `
module "db" {
  source = "./db"
  size   = var.not_declared_here
  disks  = [{ type = null }]
}
`)

	content, _ := os.ReadFile(filepath.Join(parentDir, "main.tf"))
	snapshots, _ := parseHCLContent(content, filepath.Join(parentDir, "main.tf"), make(map[string]bool), 0, nil, ParseOptions{}, nil)

	if len(snapshots) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(snapshots))
	}
	if got := snapshots[0].Attributes["instance_type"]; got != "t3.micro" {
		t.Errorf("instance_type = %v, want the default size's t3.micro", got)
	}
	want := []interface{}{map[string]interface{}{"volume_type": "gp3"}}
	if !reflect.DeepEqual(snapshots[0].Attributes["ebs_block_device"], want) {
		t.Errorf("ebs_block_device = %v, want %v", snapshots[0].Attributes["ebs_block_device"], want)
	}
}

func TestLookup_TakesANullDefaultAsTerraformDoes(t *testing.T) {
	attrs := onlyResource(t, `
resource "aws_instance" "web" {
  present      = lookup({ kms_key_id = "k1" }, "kms_key_id", null)
  missing      = lookup({ size = 10 }, "kms_key_id", null)
  with_default = lookup({ size = 10 }, "type", "gp3")
}
`)

	want := map[string]interface{}{"present": "k1", "missing": nil, "with_default": "gp3"}
	for name, value := range want {
		if attrs[name] != value {
			t.Errorf("%s = %#v, want %#v", name, attrs[name], value)
		}
	}
}

func TestLookup_ReadsAMapWithANullDefault(t *testing.T) {
	sizes := cty.MapVal(map[string]cty.Value{"size": cty.StringVal("10")})
	null := cty.NullVal(cty.DynamicPseudoType)

	found, err := lookupFunc.Call([]cty.Value{sizes, cty.StringVal("size"), null})
	if err != nil || found != cty.StringVal("10") {
		t.Errorf("lookup of a present key = %#v, %v", found, err)
	}
	missing, err := lookupFunc.Call([]cty.Value{sizes, cty.StringVal("kms_key_id"), null})
	if err != nil || !missing.IsNull() {
		t.Errorf("lookup of a missing key = %#v, %v", missing, err)
	}
}
