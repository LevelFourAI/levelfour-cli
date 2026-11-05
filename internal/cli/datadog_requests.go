package cli

import (
	"context"

	levelfourgo "github.com/LevelFourAI/levelfour-go"
	"github.com/LevelFourAI/levelfour-go/datadog"
	"github.com/spf13/pflag"
)

// datadogQuery is what the flags of a list ask for. Each list has its own SDK
// request type, so each function below maps these onto it.
type datadogQuery struct {
	page, pageSize    *int
	search            *string
	sortBy, sortOrder string
	flags             *pflag.FlagSet
}

func (q datadogQuery) strings(filter string) []string {
	values, _ := q.flags.GetStringArray(datadogFlagName(filter))
	return values
}

func (q datadogQuery) bools(filter string) []bool {
	values, _ := q.flags.GetBoolSlice(datadogFlagName(filter))
	return values
}

func (q datadogQuery) ints(filter string) []int {
	values, _ := q.flags.GetIntSlice(datadogFlagName(filter))
	return values
}

// An unset flag stays nil, which leaves the choice to the API's default.
func datadogEnum[T ~string](value string) *T {
	if value == "" {
		return nil
	}
	typed := T(value)
	return &typed
}

func listDatadogHosts(ctx context.Context, dd *datadog.Client, q datadogQuery) (any, error) {
	return dd.ListHosts(ctx, &levelfourgo.ListHostsAPIV1ProvidersDatadogHostsGetRequest{
		Sort:   datadogEnum[levelfourgo.ListHostsAPIV1ProvidersDatadogHostsGetRequestSort](q.sortBy),
		Order:  datadogEnum[levelfourgo.ListHostsAPIV1ProvidersDatadogHostsGetRequestOrder](q.sortOrder),
		Cloud:  q.strings("cloud"),
		App:    q.strings("app"),
		Team:   q.strings("team"),
		Search: q.search, Page: q.page, PageSize: q.pageSize,
	})
}

func listDatadogMetrics(ctx context.Context, dd *datadog.Client, q datadogQuery) (any, error) {
	return dd.ListMetrics(ctx, &levelfourgo.ListMetricsAPIV1ProvidersDatadogMetricsGetRequest{
		Sort:       datadogEnum[levelfourgo.ListMetricsAPIV1ProvidersDatadogMetricsGetRequestSort](q.sortBy),
		Order:      datadogEnum[levelfourgo.ListMetricsAPIV1ProvidersDatadogMetricsGetRequestOrder](q.sortOrder),
		Queried:    q.bools("queried"),
		Configured: q.bools("configured"),
		MetricType: q.strings("metric_type"),
		Search:     q.search, Page: q.page, PageSize: q.pageSize,
	})
}

func listDatadogLogIndexes(ctx context.Context, dd *datadog.Client, q datadogQuery) (any, error) {
	return dd.ListLogIndexes(ctx, &levelfourgo.ListLogIndexesAPIV1ProvidersDatadogLogIndexesGetRequest{
		Sort:          datadogEnum[levelfourgo.ListLogIndexesAPIV1ProvidersDatadogLogIndexesGetRequestSort](q.sortBy),
		Order:         datadogEnum[levelfourgo.ListLogIndexesAPIV1ProvidersDatadogLogIndexesGetRequestOrder](q.sortOrder),
		RetentionDays: q.ints("retention_days"),
		Search:        q.search, Page: q.page, PageSize: q.pageSize,
	})
}

func listDatadogLogServices(ctx context.Context, dd *datadog.Client, q datadogQuery) (any, error) {
	return dd.ListLogServices(ctx, &levelfourgo.ListLogServicesAPIV1ProvidersDatadogLogServicesGetRequest{
		Sort:   datadogEnum[levelfourgo.ListLogServicesAPIV1ProvidersDatadogLogServicesGetRequestSort](q.sortBy),
		Order:  datadogEnum[levelfourgo.ListLogServicesAPIV1ProvidersDatadogLogServicesGetRequestOrder](q.sortOrder),
		Index:  q.strings("index"),
		Search: q.search, Page: q.page, PageSize: q.pageSize,
	})
}

func listDatadogAPMServices(ctx context.Context, dd *datadog.Client, q datadogQuery) (any, error) {
	return dd.ListApmServices(ctx, &levelfourgo.ListApmServicesAPIV1ProvidersDatadogApmServicesGetRequest{
		Sort:   datadogEnum[levelfourgo.ListApmServicesAPIV1ProvidersDatadogApmServicesGetRequestSort](q.sortBy),
		Order:  datadogEnum[levelfourgo.ListApmServicesAPIV1ProvidersDatadogApmServicesGetRequestOrder](q.sortOrder),
		Env:    q.strings("env"),
		Search: q.search, Page: q.page, PageSize: q.pageSize,
	})
}

func listDatadogSynthetics(ctx context.Context, dd *datadog.Client, q datadogQuery) (any, error) {
	return dd.ListSynthetics(ctx, &levelfourgo.ListSyntheticsAPIV1ProvidersDatadogSyntheticsGetRequest{
		Sort:     datadogEnum[levelfourgo.ListSyntheticsAPIV1ProvidersDatadogSyntheticsGetRequestSort](q.sortBy),
		Order:    datadogEnum[levelfourgo.ListSyntheticsAPIV1ProvidersDatadogSyntheticsGetRequestOrder](q.sortOrder),
		TestType: q.strings("test_type"),
		Status:   q.strings(fieldStatus),
		Search:   q.search, Page: q.page, PageSize: q.pageSize,
	})
}

func listDatadogRUM(ctx context.Context, dd *datadog.Client, q datadogQuery) (any, error) {
	return dd.ListRum(ctx, &levelfourgo.ListRumAPIV1ProvidersDatadogRumGetRequest{
		Sort:            datadogEnum[levelfourgo.ListRumAPIV1ProvidersDatadogRumGetRequestSort](q.sortBy),
		Order:           datadogEnum[levelfourgo.ListRumAPIV1ProvidersDatadogRumGetRequestOrder](q.sortOrder),
		ApplicationType: q.strings("application_type"),
		Search:          q.search, Page: q.page, PageSize: q.pageSize,
	})
}

func listDatadogLogQuotas(ctx context.Context, dd *datadog.Client, q datadogQuery) (any, error) {
	return dd.ListLogQuotas(ctx, &levelfourgo.ListLogQuotasAPIV1ProvidersDatadogLogQuotasGetRequest{
		Sort:     datadogEnum[levelfourgo.ListLogQuotasAPIV1ProvidersDatadogLogQuotasGetRequestSort](q.sortBy),
		Order:    datadogEnum[levelfourgo.ListLogQuotasAPIV1ProvidersDatadogLogQuotasGetRequestOrder](q.sortOrder),
		HasQuota: q.bools("has_quota"),
		Search:   q.search, Page: q.page, PageSize: q.pageSize,
	})
}

func listDatadogMetricCardinality(ctx context.Context, dd *datadog.Client, q datadogQuery) (any, error) {
	return dd.ListMetricCardinality(ctx, &levelfourgo.ListMetricCardinalityAPIV1ProvidersDatadogMetricCardinalityGetRequest{
		Sort:    datadogEnum[levelfourgo.ListMetricCardinalityAPIV1ProvidersDatadogMetricCardinalityGetRequestSort](q.sortBy),
		Order:   datadogEnum[levelfourgo.ListMetricCardinalityAPIV1ProvidersDatadogMetricCardinalityGetRequestOrder](q.sortOrder),
		Growing: q.bools("growing"),
		Search:  q.search, Page: q.page, PageSize: q.pageSize,
	})
}

func listDatadogChanges(ctx context.Context, dd *datadog.Client, q datadogQuery) (any, error) {
	return dd.ListChanges(ctx, &levelfourgo.ListChangesAPIV1ProvidersDatadogChangesGetRequest{
		Sort:      datadogEnum[levelfourgo.ListChangesAPIV1ProvidersDatadogChangesGetRequestSort](q.sortBy),
		Order:     datadogEnum[levelfourgo.ListChangesAPIV1ProvidersDatadogChangesGetRequestOrder](q.sortOrder),
		Actor:     q.strings("actor"),
		Action:    q.strings("action"),
		AssetType: q.strings("asset_type"),
		Search:    q.search, Page: q.page, PageSize: q.pageSize,
	})
}

func listDatadogTeamCoverage(ctx context.Context, dd *datadog.Client, q datadogQuery) (any, error) {
	return dd.GetTeamCoverage(ctx, &levelfourgo.GetTeamCoverageAPIV1ProvidersDatadogTeamCoverageGetRequest{
		Sort:   datadogEnum[levelfourgo.GetTeamCoverageAPIV1ProvidersDatadogTeamCoverageGetRequestSort](q.sortBy),
		Order:  datadogEnum[levelfourgo.GetTeamCoverageAPIV1ProvidersDatadogTeamCoverageGetRequestOrder](q.sortOrder),
		Search: q.search, Page: q.page, PageSize: q.pageSize,
	})
}

func listDatadogBudgets(ctx context.Context, dd *datadog.Client, q datadogQuery) (any, error) {
	return dd.ListBudgets(ctx, &levelfourgo.ListBudgetsAPIV1ProvidersDatadogBudgetsGetRequest{
		Sort:   datadogEnum[levelfourgo.ListBudgetsAPIV1ProvidersDatadogBudgetsGetRequestSort](q.sortBy),
		Order:  datadogEnum[levelfourgo.ListBudgetsAPIV1ProvidersDatadogBudgetsGetRequestOrder](q.sortOrder),
		Scope:  q.strings("scope"),
		Search: q.search, Page: q.page, PageSize: q.pageSize,
	})
}

func listDatadogAWSResources(ctx context.Context, dd *datadog.Client, q datadogQuery) (any, error) {
	return dd.ListAwsResources(ctx, &levelfourgo.ListAwsResourcesAPIV1ProvidersDatadogAwsResourcesGetRequest{
		Sort:   datadogEnum[levelfourgo.ListAwsResourcesAPIV1ProvidersDatadogAwsResourcesGetRequestSort](q.sortBy),
		Order:  datadogEnum[levelfourgo.ListAwsResourcesAPIV1ProvidersDatadogAwsResourcesGetRequestOrder](q.sortOrder),
		Search: q.search, Page: q.page, PageSize: q.pageSize,
	})
}

func viewDatadogHost(ctx context.Context, dd *datadog.Client, id string) (any, error) {
	return dd.GetHost(ctx, id)
}

func viewDatadogMetric(ctx context.Context, dd *datadog.Client, id string) (any, error) {
	return dd.GetMetric(ctx, id)
}

func viewDatadogLogIndex(ctx context.Context, dd *datadog.Client, id string) (any, error) {
	return dd.GetLogIndex(ctx, id)
}

func viewDatadogLogService(ctx context.Context, dd *datadog.Client, id string) (any, error) {
	return dd.GetLogService(ctx, id)
}

func viewDatadogAPMService(ctx context.Context, dd *datadog.Client, id string) (any, error) {
	return dd.GetApmService(ctx, id)
}

func viewDatadogSynthetic(ctx context.Context, dd *datadog.Client, id string) (any, error) {
	return dd.GetSynthetic(ctx, id)
}

func viewDatadogRUM(ctx context.Context, dd *datadog.Client, id string) (any, error) {
	return dd.GetRumApplication(ctx, id)
}
