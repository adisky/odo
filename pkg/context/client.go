package context

import (
	"github.com/openshift/odo/pkg/kclient"
	"github.com/openshift/odo/pkg/occlient"
	"github.com/openshift/odo/pkg/odo/util"
	"github.com/spf13/cobra"
)

// Client returns an oc client configured for this command's options
func Client(command *cobra.Command) *occlient.Client {
	return client(command)
}

// ClientWithConnectionCheck returns an oc client configured for this command's options but forcing the connection check status
// to the value of the provided bool, skipping it if true, checking the connection otherwise
func ClientWithConnectionCheck(command *cobra.Command, skipConnectionCheck bool) *occlient.Client {
	return client(command)
}

// client creates an oc client based on the command flags
func client(command *cobra.Command) *occlient.Client {
	client, err := occlient.New()
	util.LogErrorAndExit(err, "")

	return client
}

// kClient creates an kclient based on the command flags
func kClient(command *cobra.Command) *kclient.Client {
	kClient, err := kclient.New()
	util.LogErrorAndExit(err, "")

	return kClient
}
