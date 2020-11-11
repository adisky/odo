package context

import (
	"os"
	"path/filepath"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/openshift/odo/pkg/config"
	"github.com/openshift/odo/pkg/envinfo"
	"github.com/openshift/odo/pkg/kclient"
	"github.com/openshift/odo/pkg/log"
	"github.com/openshift/odo/pkg/occlient"
	"github.com/openshift/odo/pkg/odo/util"
	"github.com/openshift/odo/pkg/odo/util/pushtarget"
	pkgUtil "github.com/openshift/odo/pkg/util"
)

// Ignore cluster connection
// Ignore local configuration
// CreateAppIfneeded

const (
	// DefaultAppName is the default name of the application when an application name is not provided
	DefaultAppName = "app"
	devFile        = "devfile.yaml"
)

var (
	envFile    = filepath.Join(".odo", "env", "env.yaml")
	configFile = filepath.Join(".odo", "config.yaml")
	envDir     = filepath.Join(".odo", "env")

	//EnvFilePath = filepath.Join(co.componentContext, envFile)
)

// Context
type Context struct {
	Client      *occlient.Client
	command     *cobra.Command
	Project     string
	Application string
	// component name
	cmp             string
	LocalConfigInfo *config.LocalConfigInfo
	KClient         *kclient.Client
	EnvSpecificInfo *envinfo.EnvSpecificInfo
}

type contextType string

const (
	devfileContext contextType = "devfile"
	s2iContext     contextType = "s2i"
	// for commands like list that can be run outside odo context folder
	nocontext contextType = "nocontext"
)

// NewContext creates a new Context struct populated with the current state based on flags specified for the provided command
func NewContext(command *cobra.Command, toggles ...bool) *Context {
	ignoreMissingConfig := false
	createApp := false
	if len(toggles) == 1 {
		ignoreMissingConfig = toggles[0]
	}
	if len(toggles) == 2 {
		createApp = toggles[1]
	}

	contextType := getContextType(command)

	if contextType == devfileContext {
		newDevfileContext(command, false)
	}

	return newContext(command, createApp, ignoreMissingConfig)
}

// newContext creates a new context based on the command flags, creating missing app when requested
func newContext(command *cobra.Command, createAppIfNeeded bool, ignoreMissingConfiguration bool) *Context {
	// Create a new occlient
	client := client(command)

	// Create a new kclient
	KClient, err := kclient.New()
	if err != nil {
		util.LogErrorAndExit(err, "")
	}

	// Check for valid config
	localConfiguration, err := getValidConfig(command, ignoreMissingConfiguration)
	if err != nil {
		util.LogErrorAndExit(err, "")
	}

	// Create the internal context representation based on calculated values
	internalCxt := &Context{
		Client:          client,
		command:         command,
		LocalConfigInfo: localConfiguration,
		KClient:         KClient,
	}

	internalCxt.resolveProject(localConfiguration)
	internalCxt.resolveApp(createAppIfNeeded, localConfiguration)

	// Once the component is resolved, add it to the context
	internalCxt.resolveAndSetComponent(command, localConfiguration)

	return internalCxt
}

// newDevfileContext creates a new context based on command flags for devfile components
func newDevfileContext(command *cobra.Command, createAppIfNeeded bool) *Context {

	// Create the internal context representation based on calculated values
	internalCxt := &Context{
		command: command,
		// this is only so we can make devfile and s2i work together for certain cases
		LocalConfigInfo: &config.LocalConfigInfo{},
	}

	// Get valid env information
	envInfo, err := GetValidEnvInfo(command)
	if err != nil {
		util.LogErrorAndExit(err, "")
	}

	internalCxt.EnvSpecificInfo = envInfo
	internalCxt.resolveApp(createAppIfNeeded, envInfo)

	// If the push target is NOT Docker we will set the client to Kubernetes.
	if !pushtarget.IsPushTargetDocker() {

		// Create a new kubernetes client
		internalCxt.KClient = kClient(command)
		internalCxt.Client = client(command)

		internalCxt.resolveNamespace(envInfo)
	}

	// resolve the component
	internalCxt.resolveAndSetComponent(command, envInfo)

	return internalCxt
}

// NewConfigContext is a special kind of context which only contains local configuration, other information is not retrieved
//  from the cluster. This is useful for commands which don't want to connect to cluster.
func NewConfigContext(command *cobra.Command) *Context {

	// Check for valid config
	localConfiguration, err := getValidConfig(command, false)
	if err != nil {
		util.LogErrorAndExit(err, "")
	}

	ctx := &Context{

		LocalConfigInfo: localConfiguration,
	}
	return ctx
}

// NewContextCompletion disables checking for a local configuration since when we use autocompletion on the command line, we
// couldn't care less if there was a configuration. We only need to check the parameters.
func NewContextCompletion(command *cobra.Command) *Context {
	return newContext(command, false, true)
}

// UpdatedContext returns a new context updated from config file
func UpdatedContext(context *Context) (*Context, *config.LocalConfigInfo, error) {
	localConfiguration, err := getValidConfig(context.command, false)
	return newContext(context.command, true, false), localConfiguration, err
}

func getContextType(command *cobra.Command) contextType {
	contextDir := GetContextFlagValue(command)

	s2iFlag := FlagValueIfSet(command, S2IFlagName)
	s2iflagbool, _ := strconv.ParseBool(s2iFlag)

	devfilePath := filepath.Join(contextDir, devFile)
	configPath := filepath.Join(contextDir, configFile)

	if !s2iflagbool && pkgUtil.CheckPathExists(devfilePath) {
		return devfileContext
	} else if pkgUtil.CheckPathExists(configPath) {
		return s2iContext
	}
	return nocontext
}

// Component retrieves the optionally specified component or the current one if it is set. If no component is set, exit with
// an error
func (o *Context) Component(optionalComponent ...string) string {
	return o.ComponentAllowingEmpty(false, optionalComponent...)
}

// ComponentAllowingEmpty retrieves the optionally specified component or the current one if it is set, allowing empty
// components (instead of exiting with an error) if so specified
func (o *Context) ComponentAllowingEmpty(allowEmpty bool, optionalComponent ...string) string {
	switch len(optionalComponent) {
	case 0:
		// if we're not specifying a component to resolve, get the current one (resolved in NewContext as cmp)
		// so nothing to do here unless the calling context doesn't allow no component to be set in which case we exit with error
		if !allowEmpty && len(o.cmp) == 0 {
			log.Errorf("No component is set")
			os.Exit(1)
		}
	case 1:
		cmp := optionalComponent[0]
		o.cmp = cmp
	default:
		// safeguard: fail if more than one optional string is passed because it would be a programming error
		log.Errorf("ComponentAllowingEmpty function only accepts one optional argument, was given: %v", optionalComponent)
		os.Exit(1)
	}

	return o.cmp
}
