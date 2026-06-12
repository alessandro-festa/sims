package simsconfig

const (
	APIVersionV1 = "sims.io/v1"
	KindConfig   = "SimsConfig"
)

type SimsConfig struct {
	APIVersion string   `json:"apiVersion"`
	Kind       string   `json:"kind"`
	Vendor     string   `json:"vendor"`
	Name       string   `json:"name,omitempty"`
	Workers    int      `json:"workers,omitempty"`
	K8sVersion string   `json:"k8sVersion,omitempty"`
	Taint      bool     `json:"taint,omitempty"`
	GPU        GPU      `json:"gpu"`
	Workload   Workload `json:"workload"`
	Monitoring bool     `json:"monitoring,omitempty"`
}

type GPU struct {
	Family      string   `json:"family"`
	PerWorker   int      `json:"perWorker,omitempty"`
	MemoryBytes int64    `json:"memoryBytes,omitempty"`
	Features    Features `json:"features"`
}

type Features struct {
	MIG       string    `json:"mig,omitempty"`
	Partition Partition `json:"partition"`
}

type Partition struct {
	Mode  string `json:"mode,omitempty"`
	Count int    `json:"count,omitempty"`
}

type Workload struct {
	DefaultUtilization string `json:"defaultUtilization,omitempty"`
}
