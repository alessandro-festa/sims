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
	// NVIDIA
	"GB300": {Vendor: config.VendorNVIDIA, ProductName: "B300", MemoryBytes: 288 * GiB, DisplayName: "NVIDIA B300 288GB HBM3e"},
	"GB200": {Vendor: config.VendorNVIDIA, ProductName: "B200", MemoryBytes: 192 * GiB, DisplayName: "NVIDIA B200 192GB HBM3e"},
	"H200":  {Vendor: config.VendorNVIDIA, ProductName: "H200", MemoryBytes: 141 * GiB, DisplayName: "NVIDIA H200 141GB HBM3e"},
	"H100":  {Vendor: config.VendorNVIDIA, ProductName: "H100", MemoryBytes: 80 * GiB, DisplayName: "NVIDIA H100 80GB HBM3"},
	"A100":  {Vendor: config.VendorNVIDIA, ProductName: "A100", MemoryBytes: 40 * GiB, DisplayName: "NVIDIA A100 40GB HBM2e"},
	"L40S":  {Vendor: config.VendorNVIDIA, ProductName: "L40S", MemoryBytes: 48 * GiB, DisplayName: "NVIDIA L40S 48GB GDDR6"},
	"L40":   {Vendor: config.VendorNVIDIA, ProductName: "L40", MemoryBytes: 48 * GiB, DisplayName: "NVIDIA L40 48GB GDDR6"},
	"L4":    {Vendor: config.VendorNVIDIA, ProductName: "L4", MemoryBytes: 24 * GiB, DisplayName: "NVIDIA L4 24GB GDDR6"},
	"T4":    {Vendor: config.VendorNVIDIA, ProductName: "Tesla T4", MemoryBytes: 16 * GiB, DisplayName: "NVIDIA Tesla T4 16GB GDDR6"},
	// AMD
	"MI440X": {Vendor: config.VendorAMD, ProductName: "MI440X", MemoryBytes: 432 * GiB, DisplayName: "AMD Instinct MI440X 432GB HBM4"},
	"MI430X": {Vendor: config.VendorAMD, ProductName: "MI430X", MemoryBytes: 432 * GiB, DisplayName: "AMD Instinct MI430X 432GB HBM4"},
	"MI325X": {Vendor: config.VendorAMD, ProductName: "MI325X", MemoryBytes: 256 * GiB, DisplayName: "AMD Instinct MI325X 256GB HBM3e"},
	"MI300X": {Vendor: config.VendorAMD, ProductName: "MI300X", MemoryBytes: 192 * GiB, DisplayName: "AMD Instinct MI300X 192GB HBM3"},
	"MI300A": {Vendor: config.VendorAMD, ProductName: "MI300A", MemoryBytes: 128 * GiB, DisplayName: "AMD Instinct MI300A 128GB HBM3"},
	"MI250X": {Vendor: config.VendorAMD, ProductName: "MI250X", MemoryBytes: 128 * GiB, DisplayName: "AMD Instinct MI250X 128GB HBM2e"},
	"MI250":  {Vendor: config.VendorAMD, ProductName: "MI250", MemoryBytes: 128 * GiB, DisplayName: "AMD Instinct MI250 128GB HBM2e"},
	"MI210":  {Vendor: config.VendorAMD, ProductName: "MI210", MemoryBytes: 64 * GiB, DisplayName: "AMD Instinct MI210 64GB HBM2e"},
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
