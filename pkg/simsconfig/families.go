package simsconfig

import "github.com/alessandro-festa/sims/pkg/config"

const (
	GiB = 1 << 30
)

type FamilyInfo struct {
	Vendor      string
	ProductName string
	MemoryBytes int64
	DisplayName string
}

var Families = map[string]FamilyInfo{
	"H100":  {Vendor: config.VendorNVIDIA, ProductName: "H100", MemoryBytes: 80 * GiB, DisplayName: "NVIDIA H100 80GB HBM3"},
	"A100":  {Vendor: config.VendorNVIDIA, ProductName: "A100", MemoryBytes: 40 * GiB, DisplayName: "NVIDIA A100 40GB"},
	"L4":    {Vendor: config.VendorNVIDIA, ProductName: "L4", MemoryBytes: 24 * GiB, DisplayName: "NVIDIA L4 24GB"},
	"T4":    {Vendor: config.VendorNVIDIA, ProductName: "Tesla T4", MemoryBytes: 16 * GiB, DisplayName: "NVIDIA Tesla T4 16GB"},
	"MI300X": {Vendor: config.VendorAMD, ProductName: "MI300X", MemoryBytes: 192 * GiB, DisplayName: "AMD Instinct MI300X 192GB HBM3"},
	"MI250X": {Vendor: config.VendorAMD, ProductName: "MI250X", MemoryBytes: 128 * GiB, DisplayName: "AMD Instinct MI250X 128GB HBM2e"},
	"MI100":  {Vendor: config.VendorAMD, ProductName: "MI100", MemoryBytes: 32 * GiB, DisplayName: "AMD Instinct MI100 32GB HBM2"},
}

func ApplyFamilyDefaults(cfg *SimsConfig) {
	if cfg.Workers <= 0 {
		cfg.Workers = 2
	}
	if cfg.GPU.PerWorker <= 0 {
		cfg.GPU.PerWorker = 2
	}
	if cfg.GPU.Family == "" {
		return
	}
	fam, ok := Families[cfg.GPU.Family]
	if !ok {
		return
	}
	if cfg.GPU.MemoryBytes == 0 {
		cfg.GPU.MemoryBytes = fam.MemoryBytes
	}
}
