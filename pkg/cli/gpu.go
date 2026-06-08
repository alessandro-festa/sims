package cli

import "github.com/spf13/cobra"

func newGPUCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gpu",
		Short: "Manage simulated-GPU kind clusters",
	}
	cmd.AddCommand(
		newGPUCreateCmd(),
		newGPUDeleteCmd(),
		newGPUStatusCmd(),
		newGPUDashboardCmd(),
		newGPULoadImageCmd(),
		newGPUSampleCmd(),
		newGPUMonitoringCmd(),
		newGPUDoctorCmd(),
	)
	return cmd
}
