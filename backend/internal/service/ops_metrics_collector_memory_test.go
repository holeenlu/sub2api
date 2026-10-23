package service

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/shirou/gopsutil/v4/mem"
	"github.com/stretchr/testify/require"
)

const (
	testMiB = 1024 * 1024
	testGiB = 1024 * testMiB
)

func TestReadCgroupMemoryWorkingSet(t *testing.T) {
	for _, version := range []string{"v1", "v2"} {
		for _, tc := range []struct {
			name, usage, limit, inactive string
			want                         uint64
			ok                           bool
		}{
			{"working set", "800", "1000", "300", 500, true},
			{"no cache", "800", "1000", "0", 800, true},
			{"cache ahead of usage", "800", "1000", "900", 0, true},
			{"missing cache", "800", "1000", "", 0, false},
			{"invalid cache", "800", "1000", "bad", 0, false},
			{"invalid usage", "bad", "1000", "300", 0, false},
			{"zero limit", "800", "0", "300", 0, false},
			{"unlimited", "800", "max", "300", 0, false},
			{"unlimited v1", "800", "9223372036854771712", "300", 0, false},
		} {
			t.Run(version+"/"+tc.name, func(t *testing.T) {
				root := t.TempDir()
				usagePath, limitPath, statPath, key := "memory.current", "memory.max", "memory.stat", "inactive_file"
				if version == "v1" {
					require.NoError(t, os.Mkdir(filepath.Join(root, "memory"), 0o700))
					usagePath, limitPath, statPath, key = "memory/memory.usage_in_bytes", "memory/memory.limit_in_bytes", "memory/memory.stat", "total_inactive_file"
				}
				require.NoError(t, os.WriteFile(filepath.Join(root, usagePath), []byte(tc.usage), 0o600))
				require.NoError(t, os.WriteFile(filepath.Join(root, limitPath), []byte(tc.limit), 0o600))
				if tc.inactive != "" {
					require.NoError(t, os.WriteFile(filepath.Join(root, statPath), []byte(fmt.Sprintf("anon 100\n%s\t%s\n", key, tc.inactive)), 0o600))
				}
				used, limit, ok := readCgroupMemoryBytes(root)
				require.Equal(t, tc.ok, ok)
				require.Equal(t, tc.want, used)
				if tc.ok {
					require.EqualValues(t, 1000, limit)
				} else {
					require.Zero(t, limit, "fallback must not mix container and host metrics")
				}
			})
		}
	}
}

func TestReadCgroupCPULimitRespectsSchedulableCPUs(t *testing.T) {
	for _, tc := range []struct {
		name, v2, quota, period string
		cpus                    int
		want                    float64
	}{
		{"v2 quota", "50000 100000", "", "", 2, 0.5},
		{"v2 affinity", "800000 100000", "", "", 2, 2},
		{"v1 quota", "", "50000", "100000", 2, 0.5},
		{"v1 affinity", "", "800000", "100000", 2, 2},
		{"unlimited v2", "max 100000", "", "", 2, 0},
		{"unlimited v1", "", "-1", "100000", 2, 0},
		{"invalid quota", "bad 100000", "", "", 2, 0},
		{"zero period", "100000 0", "", "", 2, 0},
		{"unknown affinity", "100000 100000", "", "", 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, os.Mkdir(filepath.Join(root, "cpu"), 0o700))
			for path, value := range map[string]string{"cpu.max": tc.v2, "cpu/cpu.cfs_quota_us": tc.quota, "cpu/cpu.cfs_period_us": tc.period} {
				if value != "" {
					require.NoError(t, os.WriteFile(filepath.Join(root, path), []byte(value), 0o600))
				}
			}
			require.Equal(t, tc.want, readCgroupCPULimitCores(root, tc.cpus))
		})
	}
}

// TestResolveMemoryStatsCgroupUsageButUnlimitedFallsBackToHost is the core
// regression: in Docker + cgroup v2 with NO memory limit, memory.current is a
// small container number while memory.max = "max" (so cgroupTotal == 0). The
// old code reported the container "used" against the host "total", producing a
// misleadingly tiny percentage (~0.3%). The fix must fall back ENTIRELY to host
// metrics instead of mixing the two sources.
func TestResolveMemoryStatsCgroupUsageButUnlimitedFallsBackToHost(t *testing.T) {
	const cgroupUsed = uint64(64573440) // memory.current, ~61 MiB
	host := &mem.VirtualMemoryStat{
		Used:        16 * testGiB,
		Total:       24 * testGiB,
		UsedPercent: 66.7,
	}

	usedMB, totalMB, pct := resolveMemoryStats(cgroupUsed, 0 /* memory.max = "max" */, true, host)

	require.NotNil(t, usedMB)
	require.NotNil(t, totalMB)
	require.NotNil(t, pct)

	// Everything must come from the host, not from the cgroup container value.
	require.Equal(t, int64(16*1024), *usedMB, "used must be host used, not container used")
	require.Equal(t, int64(24*1024), *totalMB, "total must be host total")
	require.InDelta(t, 66.7, *pct, 0.05, "percent must be host-derived, not container/host mix")

	// Guard against the specific bug: container used (~61 MiB) vs host total (~0.3%).
	require.NotEqual(t, int64(cgroupUsed/testMiB), *usedMB, "must not report the container used value")
	require.Greater(t, *pct, 1.0, "percent must not collapse to the ~0.3%% mixed value")
}

// TestResolveMemoryStatsExplicitContainerLimitUsesCgroup covers the case where
// a real container memory limit is set: memory.current = 512 MiB and
// memory.max = 2 GiB must yield ~25% entirely from cgroup, ignoring the host.
func TestResolveMemoryStatsExplicitContainerLimitUsesCgroup(t *testing.T) {
	host := &mem.VirtualMemoryStat{
		Used:        16 * testGiB, // deliberately different; must be ignored
		Total:       24 * testGiB,
		UsedPercent: 66.7,
	}

	usedMB, totalMB, pct := resolveMemoryStats(512*testMiB, 2*testGiB, true, host)

	require.NotNil(t, usedMB)
	require.NotNil(t, totalMB)
	require.NotNil(t, pct)

	require.Equal(t, int64(512), *usedMB)
	require.Equal(t, int64(2048), *totalMB)
	require.InDelta(t, 25.0, *pct, 0.05)
}

// TestResolveMemoryStatsNoCgroupUsesHost covers bare-metal / no-cgroup hosts:
// all three values come from the host reading.
func TestResolveMemoryStatsNoCgroupUsesHost(t *testing.T) {
	host := &mem.VirtualMemoryStat{
		Used:        16 * testGiB,
		Total:       24 * testGiB,
		UsedPercent: 66.7,
	}

	usedMB, totalMB, pct := resolveMemoryStats(0, 0, false, host)

	require.NotNil(t, usedMB)
	require.NotNil(t, totalMB)
	require.NotNil(t, pct)
	require.Equal(t, int64(16*1024), *usedMB)
	require.Equal(t, int64(24*1024), *totalMB)
	require.InDelta(t, 66.7, *pct, 0.05)
}

// TestResolveMemoryStatsNoDataReturnsNil: when neither cgroup nor host data is
// available, all outputs are nil (best-effort, nothing persisted).
func TestResolveMemoryStatsNoDataReturnsNil(t *testing.T) {
	usedMB, totalMB, pct := resolveMemoryStats(0, 0, false, nil)
	require.Nil(t, usedMB)
	require.Nil(t, totalMB)
	require.Nil(t, pct)
}

// TestResolveMemoryStatsHostWithoutTotalKeepsGopsutilPercent: degenerate host
// reading with no total still yields a used value and gopsutil's own percentage.
func TestResolveMemoryStatsHostWithoutTotalKeepsGopsutilPercent(t *testing.T) {
	host := &mem.VirtualMemoryStat{
		Used:        8 * testGiB,
		Total:       0,
		UsedPercent: 42.5,
	}

	usedMB, totalMB, pct := resolveMemoryStats(0, 0, false, host)

	require.NotNil(t, usedMB)
	require.Nil(t, totalMB)
	require.NotNil(t, pct)
	require.Equal(t, int64(8*1024), *usedMB)
	require.InDelta(t, 42.5, *pct, 0.05)
}
