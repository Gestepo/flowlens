package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRollupSchedulesUseExactUTCTimes(t *testing.T) {
	require.Equal(t, time.Date(2026, 7, 15, 9, 10, 0, 0, time.UTC), nextHourRollup(time.Date(2026, 7, 15, 9, 5, 0, 0, time.UTC)))
	require.Equal(t, time.Date(2026, 7, 15, 10, 10, 0, 0, time.UTC), nextHourRollup(time.Date(2026, 7, 15, 9, 11, 0, 0, time.UTC)))
	require.Equal(t, time.Date(2026, 7, 16, 0, 20, 0, 0, time.UTC), nextDayRollup(time.Date(2026, 7, 15, 0, 21, 0, 0, time.UTC)))
}
