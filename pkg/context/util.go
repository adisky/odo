package context

import (
	"fmt"
	"os"

	"github.com/openshift/odo/pkg/component"
	"github.com/openshift/odo/pkg/log"
	"github.com/openshift/odo/pkg/odo/util"
	pkgUtil "github.com/openshift/odo/pkg/util"
	"github.com/spf13/cobra"
)

// getFirstChildOfCommand gets the first child command of the root command of command
func getFirstChildOfCommand(command *cobra.Command) *cobra.Command {
	// If command does not have a parent no point checking
	if command.HasParent() {
		// Get the root command and set current command and its parent
		rootCommand := command.Root()
		parentCommand := command.Parent()
		mainCommand := command
		for {
			// if parent is root, then we have our first child in c
			if parentCommand == rootCommand {
				return mainCommand
			}
			// Traverse backwards making current command as the parent and parent as the grandparent
			mainCommand = parentCommand
			parentCommand = mainCommand.Parent()
		}
	}
	return nil
}

// checkProjectCreateOrDeleteOnlyOnInvalidNamespace errors out if user is trying to create or delete something other than project
// errFormatForCommand must contain one %s
func checkProjectCreateOrDeleteOnlyOnInvalidNamespace(command *cobra.Command, errFormatForCommand string) {
	// do not error out when its odo delete -a, so that we let users delete the local config on missing namespace
	if command.HasParent() && command.Parent().Name() != "project" && (command.Name() == "create" || (command.Name() == "delete" && !command.Flags().Changed("all"))) {
		err := fmt.Errorf(errFormatForCommand, command.Root().Name())
		util.LogErrorAndExit(err, "")
	}
}

// checkProjectCreateOrDeleteOnlyOnInvalidNamespaceNoFmt errors out if user is trying to create or delete something other than project
// compare to checkProjectCreateOrDeleteOnlyOnInvalidNamespace, no %s is needed
func checkProjectCreateOrDeleteOnlyOnInvalidNamespaceNoFmt(command *cobra.Command, errFormatForCommand string) {
	// do not error out when its odo delete -a, so that we let users delete the local config on missing namespace
	if command.HasParent() && command.Parent().Name() != "project" && (command.Name() == "create" || (command.Name() == "delete" && !command.Flags().Changed("all"))) {
		err := fmt.Errorf(errFormatForCommand)
		util.LogErrorAndExit(err, "")
	}
}

// existsOrExit checks if the specified component exists with the given context and exits the app if not.
func (o *Context) checkComponentExistsOrFail(cmp string) {
	exists, err := component.Exists(o.Client, cmp, o.Application)
	util.LogErrorAndExit(err, "")
	if !exists {
		log.Errorf("Component %v does not exist in application %s", cmp, o.Application)
		os.Exit(1)
	}
}

// ApplyIgnore will take the current ignores []string and append the mandatory odo-file-index.json and
// .git ignores; or find the .odoignore/.gitignore file in the directory and use that instead.
func ApplyIgnore(ignores *[]string, sourcePath string) (err error) {
	if len(*ignores) == 0 {
		rules, err := pkgUtil.GetIgnoreRulesFromDirectory(sourcePath)
		if err != nil {
			util.LogErrorAndExit(err, "")
		}
		*ignores = append(*ignores, rules...)
	}

	indexFile := pkgUtil.GetIndexFileRelativeToContext()
	// check if the ignores flag has the index file
	if !pkgUtil.In(*ignores, indexFile) {
		*ignores = append(*ignores, indexFile)
	}

	// check if the ignores flag has the git dir
	if !pkgUtil.In(*ignores, ".git") {
		*ignores = append(*ignores, ".git")
	}

	return nil
}
