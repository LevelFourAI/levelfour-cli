package keyring

import "github.com/zalando/go-keyring"

const service = "levelfour-cli"
const account = "api-key"

var StoreFunc = defaultStore

func defaultStore(key string) error {
	return keyring.Set(service, account, key)
}

func Store(key string) error {
	return StoreFunc(key)
}

func Get() (string, error) {
	return keyring.Get(service, account)
}

func Delete() error {
	return keyring.Delete(service, account)
}
