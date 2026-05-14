package provider

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMapRunnerTokenToDataSourceModel(t *testing.T) {
	got := mapRunnerTokenToDataSourceModel("runner-123", "abcdefghijklmnopqrstuvwxyz")

	assert.Equal(t, "runner-123", got.RunnerID.ValueString())
	assert.Equal(t, "abcdefghijklmnopqrstuvwxyz", got.ExchangeToken.ValueString())
}
