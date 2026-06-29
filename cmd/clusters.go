package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/skaledata/cli/internal/api"
	"github.com/spf13/cobra"
)

var clustersCmd = &cobra.Command{
	Use:     "clusters",
	Aliases: []string{"cluster"},
	Short:   "Manage clusters",
}

var clustersDoctorCmd = &cobra.Command{
	Use:   "doctor <cluster-id>",
	Short: "Diagnose stuck cluster state and optionally remediate",
	Long: `Inspect the cluster control plane for known stuck states that block
workloads from running — currently surfaces stale VolumeAttachment
objects left behind when a node is force-terminated without a
graceful drain (a common cause of CNPG postgres outages and
cascading airflow/airbyte CrashLoopBackOff).

Read-only by default. Pass --fix to apply safe remediations: removes
finalizers and deletes the stale VolumeAttachments so the EBS/PD/Disk
volume can attach to a new node. The underlying cloud volume is
already detached at the cloud layer — this only clears the K8s
control-plane reference.`,
	Args: cobra.ExactArgs(1),
	RunE: runClustersDoctor,
}

func init() {
	rootCmd.AddCommand(clustersCmd)
	clustersCmd.AddCommand(clustersDoctorCmd)

	clustersDoctorCmd.Flags().Bool("fix", false, "Apply safe remediations (delete stale VolumeAttachments)")
}

func runClustersDoctor(cmd *cobra.Command, args []string) error {
	clusterID := args[0]
	fix, _ := cmd.Flags().GetBool("fix")

	client, err := api.NewClient()
	if err != nil {
		return err
	}

	var diag api.ClusterDoctorResponse
	if err := client.Get("/clusters/"+clusterID+"/doctor", &diag); err != nil {
		return err
	}

	if len(diag.StaleVolumeAttachments) == 0 {
		fmt.Println("Cluster looks healthy — no stuck state detected.")
		return nil
	}

	fmt.Printf("Found %d stale VolumeAttachment(s):\n\n", len(diag.StaleVolumeAttachments))
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ATTACHMENT\tNODE (missing)\tPV\tATTACHED")
	for _, s := range diag.StaleVolumeAttachments {
		fmt.Fprintf(w, "%s\t%s\t%s\t%v\n", s.Name, s.NodeName, s.PVName, s.Attached)
	}
	_ = w.Flush()

	if !fix {
		fmt.Println("\nRun with --fix to delete these and unblock affected pods.")
		return nil
	}

	fmt.Println("\nDeleting stale VolumeAttachments...")
	var result api.ClusterDoctorFixResponse
	if err := client.Post("/clusters/"+clusterID+"/doctor/fix", nil, &result); err != nil {
		return err
	}

	fmt.Printf("Deleted %d VolumeAttachment(s).\n", len(result.DeletedVolumeAttachments))
	for _, n := range result.DeletedVolumeAttachments {
		fmt.Printf("  - %s\n", n)
	}
	fmt.Println("\nWatch pod state with: kubectl -n postgres get pods -w")
	return nil
}
