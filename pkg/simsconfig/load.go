package simsconfig

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"sigs.k8s.io/yaml"

	"github.com/alessandro-festa/sims/pkg/config"
)

func Load(path string) (*SimsConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	return Parse(data)
}

func Parse(data []byte) (*SimsConfig, error) {
	var cfg SimsConfig
	if err := yaml.UnmarshalStrict(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if err := validate(&cfg); err != nil {
		return nil, err
	}
	ApplyFamilyDefaults(&cfg)
	if cfg.TaintedWorkers > cfg.Workers {
		return nil, fmt.Errorf("taintedWorkers (%d) must be <= workers (%d)", cfg.TaintedWorkers, cfg.Workers)
	}
	return &cfg, nil
}

func validate(cfg *SimsConfig) error {
	if cfg.APIVersion != APIVersionV1 {
		return fmt.Errorf("apiVersion must be %q, got %q", APIVersionV1, cfg.APIVersion)
	}
	if cfg.Kind != KindConfig {
		return fmt.Errorf("kind must be %q, got %q", KindConfig, cfg.Kind)
	}
	switch cfg.Vendor {
	case config.VendorNVIDIA, config.VendorAMD:
	default:
		return fmt.Errorf("invalid vendor %q (must be %q or %q)", cfg.Vendor, config.VendorNVIDIA, config.VendorAMD)
	}

	if cfg.GPU.Family != "" {
		fam, ok := Families[cfg.GPU.Family]
		if !ok {
			return fmt.Errorf("unknown GPU family %q", cfg.GPU.Family)
		}
		if fam.Vendor != cfg.Vendor {
			return fmt.Errorf("GPU family %q is %s, but vendor is %q", cfg.GPU.Family, fam.Vendor, cfg.Vendor)
		}
	}

	if cfg.GPU.Features.MIG != "" && cfg.Vendor != config.VendorNVIDIA {
		return fmt.Errorf("features.mig is NVIDIA-only, but vendor is %q", cfg.Vendor)
	}
	if (cfg.GPU.Features.Partition.Mode != "" || cfg.GPU.Features.Partition.Count != 0) && cfg.Vendor != config.VendorAMD {
		return fmt.Errorf("features.partition is AMD-only, but vendor is %q", cfg.Vendor)
	}
	if m := cfg.GPU.Features.Partition.Mode; m != "" {
		if m != "spx" && m != "cpx" {
			return fmt.Errorf("features.partition.mode must be \"spx\" or \"cpx\", got %q", m)
		}
	}
	if c := cfg.GPU.Features.Partition.Count; c != 0 {
		if c < 1 || c > 8 {
			return fmt.Errorf("features.partition.count must be 1-8, got %d", c)
		}
	}

	if u := cfg.Workload.DefaultUtilization; u != "" {
		if err := validateUtilRange(u); err != nil {
			return fmt.Errorf("workload.defaultUtilization: %w", err)
		}
	}

	if cfg.TaintedWorkers < 0 {
		return fmt.Errorf("taintedWorkers must be >= 0, got %d", cfg.TaintedWorkers)
	}

	return nil
}

func validateUtilRange(s string) error {
	parts := strings.SplitN(s, "-", 2)
	if len(parts) != 2 {
		return fmt.Errorf("must be \"N-M\" (e.g. \"5-15\"), got %q", s)
	}
	lo, err := strconv.Atoi(parts[0])
	if err != nil {
		return fmt.Errorf("low bound %q is not an integer", parts[0])
	}
	hi, err := strconv.Atoi(parts[1])
	if err != nil {
		return fmt.Errorf("high bound %q is not an integer", parts[1])
	}
	if lo < 0 || lo > 100 || hi < 0 || hi > 100 {
		return fmt.Errorf("bounds must be 0-100, got %d-%d", lo, hi)
	}
	if lo > hi {
		return fmt.Errorf("low bound (%d) must be <= high bound (%d)", lo, hi)
	}
	return nil
}
