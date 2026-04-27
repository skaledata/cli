package cmd

import (
	"fmt"

	"github.com/skaledata/cli/internal/api"
	"github.com/spf13/cobra"
)

var billingCmd = &cobra.Command{
	Use:   "billing",
	Short: "Show billing and subscription info",
	RunE:  runBilling,
}

func init() {
	rootCmd.AddCommand(billingCmd)
}

func runBilling(cmd *cobra.Command, args []string) error {
	client, err := api.NewClient()
	if err != nil {
		return err
	}

	var sub api.SubscriptionResponse
	if err := client.Get("/billing/subscription", &sub); err != nil {
		return err
	}

	fmt.Printf("Plan:   %s\n", sub.PlanName)
	fmt.Printf("Status: %s\n", sub.Status)
	if sub.MaxClusters != nil {
		fmt.Printf("Cluster limit: %d\n", *sub.MaxClusters)
	}
	if sub.TrialDaysRemaining != nil {
		fmt.Printf("Trial days remaining: %d\n", *sub.TrialDaysRemaining)
	}
	if sub.BillingExempt {
		fmt.Println("Billing exempt: yes")
	}
	return nil
}
