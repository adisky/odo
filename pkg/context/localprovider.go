package context

import (
	"fmt"

	"github.com/openshift/odo/pkg/config"
	"github.com/openshift/odo/pkg/envinfo"
	"github.com/openshift/odo/pkg/odo/util"
	pkgUtil "github.com/openshift/odo/pkg/util"
	"github.com/spf13/cobra"
	"k8s.io/klog"
)

// getValidEnvInfo accesses the environment file
func GetValidEnvInfo(command *cobra.Command) (*envinfo.EnvSpecificInfo, error) {

	// Get details from the env file
	componentContext := FlagValueIfSet(command, ContextFlagName)

	// Grab the absolute path of the env file
	if componentContext != "" {
		fAbs, err := pkgUtil.GetAbsPath(componentContext)
		util.LogErrorAndExit(err, "")
		componentContext = fAbs
	} else {
		fAbs, err := pkgUtil.GetAbsPath(".")
		util.LogErrorAndExit(err, "")
		componentContext = fAbs
	}

	// Access the env file
	envInfo, err := envinfo.NewEnvSpecificInfo(componentContext)
	if err != nil {
		return nil, err
	}

	// Now we check to see if we can skip gathering the information.
	// Return if we can skip gathering configuration information
	canWeSkip, err := checkIfConfigurationNeeded(command)
	if err != nil {
		return nil, err
	}
	if canWeSkip {
		return envInfo, nil
	}

	// Check to see if the environment file exists
	if !envInfo.Exists() {
		return nil, fmt.Errorf("The current directory does not represent an odo component. Use 'odo create' to create component here or switch to directory with a component")
	}

	return envInfo, nil
}

func getValidConfig(command *cobra.Command, ignoreMissingConfiguration bool) (*config.LocalConfigInfo, error) {

	// Get details from the local config file
	contextDir := GetContextFlagValue(command)

	// Access the local configuration
	localConfiguration, err := config.NewLocalConfigInfo(contextDir)
	if err != nil {
		return nil, err
	}

	// Now we check to see if we can skip gathering the information.
	// If true, we just return.
	canWeSkip, err := checkIfConfigurationNeeded(command)
	if err != nil {
		return nil, err
	}
	if canWeSkip {
		return localConfiguration, nil
	}

	// If file does not exist at this point, raise an error
	// HOWEVER..
	// When using auto-completion, we should NOT error out, just ignore the fact that there is no configuration
	if !localConfiguration.Exists() && ignoreMissingConfiguration {
		klog.V(4).Info("There is NO config file that exists, we are however ignoring this as the ignoreMissingConfiguration flag has been passed in as true")
	} else if !localConfiguration.Exists() {
		return nil, fmt.Errorf("The current directory does not represent an odo component. Use 'odo create' to create component here or switch to directory with a component")
	}

	// else simply return the local config info
	return localConfiguration, nil
}

// checkIfConfigurationNeeded checks against a set of commands that do *NOT* need configuration.
func checkIfConfigurationNeeded(command *cobra.Command) (bool, error) {

	// Here we will check for parent commands, if the match a certain criteria, we will skip
	// using the configuration.
	//
	// For example, `odo create` should NOT check to see if there is actually a configuration yet.
	if command.HasParent() {

		// Gather necessary preliminary information
		parentCommand := command.Parent()
		rootCommand := command.Root()
		flagValue := FlagValueIfSet(command, ApplicationFlagName)

		// Find the first child of the command, as some groups are allowed even with non existent configuration
		firstChildCommand := getFirstChildOfCommand(command)

		// This should *never* happen, but added just to be safe
		if firstChildCommand == nil {
			return false, fmt.Errorf("Unable to get first child of command")
		}
		// Case 1 : if command is create operation just allow it
		if command.Name() == "create" && (parentCommand.Name() == "component" || parentCommand.Name() == rootCommand.Name()) {
			return true, nil
		}
		// Case 2 : if command is describe or delete and app flag is used just allow it
		if (firstChildCommand.Name() == "describe" || firstChildCommand.Name() == "delete") && len(flagValue) > 0 {
			return true, nil
		}
		// Case 3 : if command is list, just allow it
		if firstChildCommand.Name() == "list" {
			return true, nil
		}
		// Case 4 : Check if firstChildCommand is project. If so, skip validation of context
		if firstChildCommand.Name() == "project" {
			return true, nil
		}
		// Case 5 : Check if specific flags are set for specific first child commands
		if firstChildCommand.Name() == "app" {
			return true, nil
		}
		// Case 6 : Check if firstChildCommand is catalog and request is to list or search
		if firstChildCommand.Name() == "catalog" && (parentCommand.Name() == "list" || parentCommand.Name() == "search") {
			return true, nil
		}
		// Case 7: Check if firstChildCommand is component and  request is list
		if (firstChildCommand.Name() == "component" || firstChildCommand.Name() == "service") && command.Name() == "list" {
			return true, nil
		}
		// Case 8 : Check if firstChildCommand is component and app flag is used
		if firstChildCommand.Name() == "component" && len(flagValue) > 0 {
			return true, nil
		}
		// Case 9 : Check if firstChildCommand is logout and app flag is used
		if firstChildCommand.Name() == "logout" {
			return true, nil
		}
		// Case 10: Check if firstChildCommand is service and command is create or delete. Allow it if that's the case
		if firstChildCommand.Name() == "service" && (command.Name() == "create" || command.Name() == "delete") {
			return true, nil
		}

	} else {
		return true, nil
	}

	return false, nil
}
