package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

const sampleNVIDIA = `apiVersion: v1
kind: Pod
metadata:
  name: sims-nvidia-sample
  annotations:
    run.ai/simulated-gpu-utilization: "30-50"
spec:
  restartPolicy: Never
  containers:
    - name: payload
      image: busybox
      command: ["sh", "-c", "echo 'sims NVIDIA sample running'; sleep 3600"]
      resources:
        limits:
          nvidia.com/gpu: 1
`

const sampleAMD = `apiVersion: v1
kind: Pod
metadata:
  name: sims-amd-sample
  annotations:
    sims.io/simulated-gpu-utilization: "30-50"
spec:
  restartPolicy: Never
  containers:
    - name: payload
      image: busybox
      command: ["sh", "-c", "echo 'sims AMD sample running'; sleep 3600"]
      resources:
        limits:
          amd.com/gpu: 1
`

func newGPUSampleCmd() *cobra.Command {
	var vendor string
	cmd := &cobra.Command{
		Use:   "sample",
		Short: "Print a sample GPU-requesting Pod manifest for the chosen vendor",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateVendor(vendor); err != nil {
				return err
			}
			switch vendor {
			case "nvidia":
				fmt.Print(sampleNVIDIA)
			case "amd":
				fmt.Print(sampleAMD)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&vendor, "vendor", "", "GPU vendor (nvidia|amd)")
	_ = cmd.MarkFlagRequired("vendor")
	return cmd
}
