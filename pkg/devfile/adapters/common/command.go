package common

import (
	"fmt"
	"reflect"

	"github.com/openshift/odo/pkg/devfile/parser/data"
	"github.com/openshift/odo/pkg/devfile/parser/data/common"
	"k8s.io/klog"
)

// GetCommand iterates through the devfile commands and returns the associated devfile command
func getCommand(data data.DevfileData, commandName string, groupType common.DevfileCommandGroupType, required bool) (supportedCommand common.DevfileCommand, err error) {

	for _, command := range data.GetCommands() {
		// validate command
		err = validateCommand(data, command)

		if err != nil {
			return common.DevfileCommand{}, err
		}

		// if command is specified via flags, it has the highest priority
		// search through all commands to find the specified command name
		// if not found fallback to error.
		if commandName != "" {

			if command.Exec.Id == commandName {

				if supportedCommand.Exec.Group.Kind == "" {
					// Devfile V1 for commands passed from flags
					// Group type is not updated during conversion
					command.Exec.Group.Kind = groupType
				}

				// we have found the command with name, its groupType Should match to the flag
				// e.g --build-command "mybuild"
				// exec:
				//   id: mybuild
				// group:
				//   kind: build
				if command.Exec.Group.Kind != groupType {
					return supportedCommand, fmt.Errorf("mismatched type, command %s is of type %v groupType in devfile", commandName, groupType)

				}
				supportedCommand = command
				return supportedCommand, nil
			}
			continue
		}

		// if not command specified via flag, default command has the highest priority
		if command.Exec.Group.Kind == groupType && command.Exec.Group.IsDefault {
			supportedCommand = command
			return supportedCommand, nil
		}

		// return the first command found for the matching type.
		if command.Exec.Group.Kind == groupType {
			supportedCommand = command
			return supportedCommand, nil
		}
	}

	// The command was not found
	msg := fmt.Sprintf("The command \"%v\" was not found in the devfile", commandName)
	if required {
		// Not found and required, return an error
		err = fmt.Errorf(msg)
	} else {
		// Not found and optional, so just log it
		klog.V(3).Info(msg)
	}

	return
}

// validateCommand validates the given command
// 1. command has to be of type exec
// 2. component should be present
// 3. command should be present
func validateCommand(data data.DevfileData, command common.DevfileCommand) (err error) {

	// type must be exec
	if command.Exec == nil {
		return fmt.Errorf("Command must be of type \"exec\"")
	}

	// component must be specified
	if &command.Exec.Component == nil || command.Exec.Component == "" {
		return fmt.Errorf("Exec commands must reference a component")
	}

	// must specify a command
	if &command.Exec.CommandLine == nil || command.Exec.CommandLine == "" {
		return fmt.Errorf("Exec commands must have a command")
	}

	// must map to a supported component
	components := GetSupportedComponents(data)

	isActionValid := false
	for _, component := range components {
		if command.Exec.Component == component.Container.Name {
			isActionValid = true
		}
	}
	if !isActionValid {
		return fmt.Errorf("the command does not map to a supported component")
	}

	return
}

// GetInitCommand iterates through the components in the devfile and returns the init command
func GetInitCommand(data data.DevfileData, devfileInitCmd string) (initCommand common.DevfileCommand, err error) {

	if devfileInitCmd != "" {
		// a init command was specified so if it is not found then it is an error
		return getCommand(data, devfileInitCmd, common.InitCommandGroupType, true)
	}
	// a init command was not specified so if it is not found then it is not an error
	return getCommand(data, "", common.InitCommandGroupType, false)
}

// GetBuildCommand iterates through the components in the devfile and returns the build command
func GetBuildCommand(data data.DevfileData, devfileBuildCmd string) (buildCommand common.DevfileCommand, err error) {
	if devfileBuildCmd != "" {
		// a build command was specified so if it is not found then it is an error
		return getCommand(data, devfileBuildCmd, common.BuildCommandGroupType, true)
	}
	// a build command was not specified so if it is not found then it is not an error
	return getCommand(data, "", common.BuildCommandGroupType, false)
}

// GetRunCommand iterates through the components in the devfile and returns the run command
func GetRunCommand(data data.DevfileData, devfileRunCmd string) (runCommand common.DevfileCommand, err error) {
	if devfileRunCmd != "" {
		return getCommand(data, devfileRunCmd, common.RunCommandGroupType, true)
	}
	return getCommand(data, "", common.RunCommandGroupType, true)
}

// ValidateAndGetPushDevfileCommands validates the build and the run command,
// if provided through odo push or else checks the devfile for devBuild and devRun.
// It returns the build and run commands if its validated successfully, error otherwise.
func ValidateAndGetPushDevfileCommands(data data.DevfileData, devfileInitCmd, devfileBuildCmd, devfileRunCmd string) (commandMap PushCommandsMap, err error) {
	var emptyCommand common.DevfileCommand
	commandMap = NewPushCommandMap()

	isInitCommandValid, isBuildCommandValid, isRunCommandValid := false, false, false

	initCommand, initCmdErr := GetInitCommand(data, devfileInitCmd)

	isInitCmdEmpty := reflect.DeepEqual(emptyCommand, initCommand)
	if isInitCmdEmpty && initCmdErr == nil {
		// If there was no init command specified through odo push and no default init command in the devfile, default validate to true since the init command is optional
		isInitCommandValid = true
		klog.V(3).Infof("No init command was provided")
	} else if !isInitCmdEmpty && initCmdErr == nil {
		isInitCommandValid = true
		commandMap[common.InitCommandGroupType] = initCommand
		klog.V(3).Infof("Init command: %v", initCommand.Exec.Id)
	}

	buildCommand, buildCmdErr := GetBuildCommand(data, devfileBuildCmd)

	isBuildCmdEmpty := reflect.DeepEqual(emptyCommand, buildCommand)
	if isBuildCmdEmpty && buildCmdErr == nil {
		// If there was no build command specified through odo push and no default build command in the devfile, default validate to true since the build command is optional
		isBuildCommandValid = true
		klog.V(3).Infof("No build command was provided")
	} else if !reflect.DeepEqual(emptyCommand, buildCommand) && buildCmdErr == nil {
		isBuildCommandValid = true
		commandMap[common.BuildCommandGroupType] = buildCommand
		klog.V(3).Infof("Build command: %v", buildCommand.Exec.Id)
	}

	runCommand, runCmdErr := GetRunCommand(data, devfileRunCmd)
	if runCmdErr == nil && !reflect.DeepEqual(emptyCommand, runCommand) {
		isRunCommandValid = true
		commandMap[common.RunCommandGroupType] = runCommand
		klog.V(3).Infof("Run command: %v", runCommand.Exec.Id)
	}

	// If either command had a problem, return an empty list of commands and an error
	if !isInitCommandValid || !isBuildCommandValid || !isRunCommandValid {
		commandErrors := ""
		if initCmdErr != nil {
			commandErrors += fmt.Sprintf(initCmdErr.Error(), "\n")
		}
		if buildCmdErr != nil {
			commandErrors += fmt.Sprintf(buildCmdErr.Error(), "\n")
		}
		if runCmdErr != nil {
			commandErrors += fmt.Sprintf(runCmdErr.Error(), "\n")
		}
		return commandMap, fmt.Errorf(commandErrors)
	}

	return commandMap, nil
}
