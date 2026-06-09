// Command rocm-smi-mock is a stand-in for AMD's `rocm-smi` that workloads
// can shell out to inside a sims cluster. Real rocm-smi panics or refuses
// to start without a real AMDGPU driver (run-ai/fake-gpu-operator's
// nvidia-smi panic is the analog), so any init script that calls
// `rocm-smi` to enumerate devices fails. This binary returns a credible
// fake — enough to satisfy tooling that just needs "rocm-smi exits 0 and
// prints something parseable."
//
// Two output modes:
//
//   - default (no args) → concise tabular layout matching `rocm-smi`'s
//     own default. One row per fake GPU.
//   - --json → the dict layout `rocm-smi --json` produces (key per
//     card, values as strings). Keys mirror real rocm-smi exactly so
//     standard ROCm-aware parsers work.
//
// The binary is stdlib-only (no k8s client) so it can be mounted into
// arbitrary user containers as a tiny static binary. Configuration
// (gpus-per-node, product, partition mode) comes from flags / env, with
// sensible defaults that match the sims chart.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
)

const driverVersion = "5.7.1"
const cardVendor = "Advanced Micro Devices, Inc."

type cardInfo struct {
	Index        int
	UUID         string
	Serial       string
	Product      string
	TempC        float64
	PowerW       float64
	PowerMaxW    float64
	SCLKMHz      int
	MCLKMHz      int
	FanPercent   int
	VRAMUsedPct  int
	GPUUsedPct   int
	VRAMTotalB   int64
	PartitionMode string
	PartitionID  string
}

func main() {
	fs := flag.NewFlagSet("rocm-smi-mock", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "Emit the dict format `rocm-smi --json` produces instead of the concise table.")
	gpus := fs.Int("gpus-per-node", envInt("ROCM_SMI_MOCK_GPUS_PER_NODE", 2), "Number of physical fake GPUs to report. Env: ROCM_SMI_MOCK_GPUS_PER_NODE.")
	product := fs.String("product-name", envStr("ROCM_SMI_MOCK_PRODUCT_NAME", "MI300X"), "Card model name. Env: ROCM_SMI_MOCK_PRODUCT_NAME.")
	memBytes := fs.Int64("memory-bytes", envInt64("ROCM_SMI_MOCK_MEMORY_BYTES", 206158430208), "Per-physical-GPU total VRAM in bytes. Env: ROCM_SMI_MOCK_MEMORY_BYTES.")
	partitionMode := fs.String("partition-mode", envStr("ROCM_SMI_MOCK_PARTITION_MODE", "spx"), "CPX/SPX partition mode. Env: ROCM_SMI_MOCK_PARTITION_MODE.")
	partitionCount := fs.Int("partition-count", envInt("ROCM_SMI_MOCK_PARTITION_COUNT", 1), "Partitions per physical GPU under CPX. Env: ROCM_SMI_MOCK_PARTITION_COUNT.")
	_ = fs.Parse(os.Args[1:])

	cards := buildCards(*gpus, *product, *memBytes, *partitionMode, *partitionCount)
	if *jsonOut {
		printJSON(os.Stdout, cards)
	} else {
		printTable(os.Stdout, cards)
	}
}

func buildCards(physical int, product string, memBytes int64, partMode string, partCount int) []cardInfo {
	if partMode != "cpx" {
		partMode = "spx"
		partCount = 1
	}
	if partCount < 1 {
		partCount = 1
	}
	perPart := memBytes / int64(partCount)
	out := make([]cardInfo, 0, physical*partCount)
	for phys := range physical {
		for part := range partCount {
			global := phys*partCount + part
			out = append(out, cardInfo{
				Index:         global,
				UUID:          fmt.Sprintf("GPU-sim-%02d-p%d", phys, part),
				Serial:        fmt.Sprintf("SIM-%02d-p%d", phys, part),
				Product:       product,
				TempC:         35.0,
				PowerW:        30.0,
				PowerMaxW:     750.0,
				SCLKMHz:       500,
				MCLKMHz:       1600,
				FanPercent:    0,
				VRAMUsedPct:   0,
				GPUUsedPct:    0,
				VRAMTotalB:    perPart,
				PartitionMode: partMode,
				PartitionID:   strconv.Itoa(part),
			})
		}
	}
	return out
}

// printTable mimics `rocm-smi` (no args). Exact column set + spacing matches
// what real rocm-smi prints on a single-GPU node so screen-scraping
// parsers see the shape they expect.
func printTable(w io.Writer, cards []cardInfo) {
	fmt.Fprintln(w, "======================== ROCm System Management Interface ========================")
	fmt.Fprintln(w, "==================================== Concise Info ====================================")

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "GPU\tTemp(C)\tAvgPwr\tSCLK\tMCLK\tFan\tPerf\tPwrCap\tVRAM%\tGPU%")
	for _, c := range cards {
		fmt.Fprintf(tw, "%d\t%.1f\t%.1fW\t%dMhz\t%dMhz\t%d%%\tauto\t%.1fW\t%d%%\t%d%%\n",
			c.Index, c.TempC, c.PowerW, c.SCLKMHz, c.MCLKMHz, c.FanPercent, c.PowerMaxW, c.VRAMUsedPct, c.GPUUsedPct)
	}
	_ = tw.Flush()

	fmt.Fprintln(w, strings.Repeat("=", 80))
	fmt.Fprintln(w, "============================== End of ROCm SMI Log ===============================")
}

// printJSON produces the `rocm-smi --json` dict format. The "system" key
// + "cardN" keys mirror real rocm-smi exactly; common keys inside each
// card use the same human-readable strings real rocm-smi emits.
func printJSON(w io.Writer, cards []cardInfo) {
	doc := map[string]any{
		"system": map[string]string{
			"Driver version": driverVersion,
		},
	}
	for _, c := range cards {
		doc[fmt.Sprintf("card%d", c.Index)] = map[string]string{
			"GUID":                                 c.UUID,
			"Card Series":                          c.Product,
			"Card Model":                           c.Product,
			"Card SKU":                             c.Product,
			"Card Vendor":                          cardVendor,
			"GPU ID":                               "0x74a1",
			"Subsystem ID":                         "0x0c34",
			"Serial Number":                        c.Serial,
			"Temperature (Sensor edge) (C)":        fmt.Sprintf("%.1f", c.TempC),
			"Temperature (Sensor junction) (C)":    fmt.Sprintf("%.1f", c.TempC),
			"Temperature (Sensor memory) (C)":      fmt.Sprintf("%.1f", c.TempC),
			"Average Graphics Package Power (W)":   fmt.Sprintf("%.1f", c.PowerW),
			"Max Graphics Package Power (W)":       fmt.Sprintf("%.1f", c.PowerMaxW),
			"Performance Level":                    "auto",
			"GPU use (%)":                          strconv.Itoa(c.GPUUsedPct),
			"GFX Activity":                         strconv.Itoa(c.GPUUsedPct),
			"GPU Memory Allocated (VRAM%)":         strconv.Itoa(c.VRAMUsedPct),
			"GPU Memory Read/Write Activity (%)":   "0",
			"GPU memory vendor":                    "HBM3",
			"PCIe Replay Count":                    "0",
			"Voltage (mV)":                         "800",
			"PCI Bus":                              fmt.Sprintf("0000:00:%02d.0", c.Index),
			"VRAM Total Memory (B)":                strconv.FormatInt(c.VRAMTotalB, 10),
			// sims-only extras — partition labels mirror what the
			// metrics-exporter stamps so users correlating rocm-smi
			// output to Grafana series can join on these.
			"sims.io Partition Mode": c.PartitionMode,
			"sims.io Partition ID":   c.PartitionID,
		}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "    ")
	_ = enc.Encode(doc)
}

func envStr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return def
}

func envInt64(key string, def int64) int64 {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.ParseInt(v, 10, 64); err == nil {
			return i
		}
	}
	return def
}
