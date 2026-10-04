package cli

import (
	"encoding/json"
	"errors"

	"github.com/LevelFourAI/levelfour-cli/internal/api"
	"github.com/LevelFourAI/levelfour-cli/internal/output"
)

const (
	granularityMonthly = "monthly"

	// The series a chart folds its smaller groups into.
	otherSeries = "__other__"
)

// An API that predates the projection answers the chart without one, which reads the same as a
// provider with nothing to project from unless the answer is checked for the field itself.
var errForecastNotProjected = errors.New("the API answered without a forecast: it does not project one yet")

// A bound the answer leaves out prints as none. Zero would read as a real floor.
type forecastBounds struct {
	Value *float64 `json:"value"`
	Low   *float64 `json:"low"`
	High  *float64 `json:"high"`
}

type forecastPoint struct {
	forecastBounds
	Date   string                    `json:"date"`
	Groups map[string]forecastBounds `json:"groups"`
}

type projectedChart struct {
	ProviderName        string            `json:"provider_name"`
	Groups              []string          `json:"groups"`
	GroupLabels         map[string]string `json:"group_labels"`
	DataCompleteThrough string            `json:"data_complete_through"`
	Measure             string            `json:"measure"`
	Unit                string            `json:"unit"`
	// Kept as sent: an answer with no forecast field and one whose forecast is null mean different
	// things, and a decoded pointer is nil for both.
	Forecast json.RawMessage `json:"forecast"`
}

func (c projectedChart) seriesName(key string) string {
	if key == otherSeries {
		return "Other"
	}
	if label := c.GroupLabels[key]; label != "" {
		return label
	}
	return key
}

func runCostsForecast(client *api.SDKClient, providerID string, state costsFilterState) error {
	if state.granularity == "" {
		state.granularity = granularityMonthly
	}
	body, err := getBreakdown(client, providerID, buildCostsRawParams(providerID, state, "chart"))
	if err != nil {
		return err
	}
	chart, points, err := decodeProjectedChart(body)
	if err != nil {
		return err
	}
	figures := measured{measure: chart.Measure, unit: chart.Unit}
	if state.measure == measureUsage && !figures.isUsage() {
		return errUsageNotMeasured
	}
	// Checked first: a script reading the raw answer asked for a projection, and a quantity, too.
	if output.HasFormattingFlags() {
		output.PrintRaw(string(body))
		return nil
	}
	renderForecast(chart, points, figures)
	return nil
}

// The chart and the buckets it projects. No buckets means the API projects and had nothing to
// project from.
func decodeProjectedChart(body []byte) (projectedChart, []forecastPoint, error) {
	var answer struct {
		Data *projectedChart `json:"data"`
	}
	if err := json.Unmarshal(body, &answer); err != nil || answer.Data == nil {
		return projectedChart{}, nil, errUnexpectedBody(body)
	}
	if answer.Data.Forecast == nil {
		return projectedChart{}, nil, errForecastNotProjected
	}
	var forecast struct {
		DataPoints []forecastPoint `json:"data_points"`
	}
	if err := json.Unmarshal(answer.Data.Forecast, &forecast); err != nil {
		return projectedChart{}, nil, errUnexpectedBody(body)
	}
	return *answer.Data, forecast.DataPoints, nil
}

func renderForecast(chart projectedChart, points []forecastPoint, figures measured) {
	cards := []output.KPICard{{Label: "Provider", Value: chart.ProviderName}}
	if chart.DataCompleteThrough != "" {
		cards = append(cards, output.KPICard{Label: "Complete through", Value: chart.DataCompleteThrough})
	}
	if figures.isUsage() {
		cards = append(cards, output.KPICard{Label: "Measured in", Value: figures.unit})
	}
	output.KPICards(cards)

	if len(points) == 0 {
		output.Info("Nothing to project from.")
		return
	}
	rows := make([][]string, 0, len(points)*(len(chart.Groups)+1))
	for _, point := range points {
		rows = append(rows, forecastRow(point.Date, "Total", point.forecastBounds, figures))
		for _, key := range chart.Groups {
			if bounds, ok := point.Groups[key]; ok {
				rows = append(rows, forecastRow(point.Date, chart.seriesName(key), bounds, figures))
			}
		}
	}
	output.Table([]string{"Period", "Series", "Projected", "Low", "High"}, rows)
	output.Info("Projected at the median daily rate of the last 30 complete days. Low and high are its 25th and 75th percentiles.")
}

func forecastRow(period, series string, bounds forecastBounds, figures measured) []string {
	return []string{
		period,
		series,
		figures.sent(bounds.Value),
		figures.sent(bounds.Low),
		figures.sent(bounds.High),
	}
}
