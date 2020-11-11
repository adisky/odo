package context

import (
	"github.com/openshift/odo/pkg/odo/util"
	pkgUtil "github.com/openshift/odo/pkg/util"
	"github.com/spf13/cobra"
)

const (
	// ProjectFlagName is the name of the flag allowing a user to specify which project to operate on
	ProjectFlagName = "project"
	// ApplicationFlagName is the name of the flag allowing a user to specify which application to operate on
	ApplicationFlagName = "app"
	// ComponentFlagName is the name of the flag allowing a user to specify which component to operate on
	ComponentFlagName = "component"
	// OutputFlagName is the name of the flag allowing user to specify output format
	OutputFlagName = "output"
	// ContextFlagName is the name of the flag allowing a user to specify the location of the component settings
	ContextFlagName = "context"
	// S2IFlagName is the name of the flag used to force s2i options
	S2IFlagName = "s2i"
)

// FlagValueIfSet retrieves the value of the specified flag if it is set for the given command
func FlagValueIfSet(cmd *cobra.Command, flagName string) string {
	flag, _ := cmd.Flags().GetString(flagName)
	return flag
}

func GetContextFlagValue(command *cobra.Command) string {
	contextDir := FlagValueIfSet(command, ContextFlagName)

	// Grab the absolute path of the configuration
	if contextDir != "" {
		fAbs, err := pkgUtil.GetAbsPath(contextDir)
		util.LogErrorAndExit(err, "")
		contextDir = fAbs
	} else {
		fAbs, err := pkgUtil.GetAbsPath(".")
		util.LogErrorAndExit(err, "")
		contextDir = fAbs
	}
	return contextDir
}
