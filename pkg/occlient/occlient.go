package occlient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"

	"github.com/openshift/odo/pkg/config"
	"github.com/openshift/odo/pkg/devfile/adapters/common"
	"github.com/openshift/odo/pkg/kclient"
	"github.com/openshift/odo/pkg/log"
	"github.com/openshift/odo/pkg/preference"
	"github.com/openshift/odo/pkg/util"

	// api clientsets
	servicecatalogclienset "github.com/kubernetes-sigs/service-catalog/pkg/client/clientset_generated/clientset/typed/servicecatalog/v1beta1"
	appsclientset "github.com/openshift/client-go/apps/clientset/versioned/typed/apps/v1"
	buildschema "github.com/openshift/client-go/build/clientset/versioned/scheme"
	buildclientset "github.com/openshift/client-go/build/clientset/versioned/typed/build/v1"
	imageclientset "github.com/openshift/client-go/image/clientset/versioned/typed/image/v1"
	projectclientset "github.com/openshift/client-go/project/clientset/versioned/typed/project/v1"
	routeclientset "github.com/openshift/client-go/route/clientset/versioned/typed/route/v1"
	userclientset "github.com/openshift/client-go/user/clientset/versioned/typed/user/v1"

	// api resource types
	scv1beta1 "github.com/kubernetes-sigs/service-catalog/pkg/apis/servicecatalog/v1beta1"
	appsv1 "github.com/openshift/api/apps/v1"
	buildv1 "github.com/openshift/api/build/v1"
	dockerapiv10 "github.com/openshift/api/image/docker10"
	imagev1 "github.com/openshift/api/image/v1"
	oauthv1client "github.com/openshift/client-go/oauth/clientset/versioned/typed/oauth/v1"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/klog"
)

var (
	DEPLOYMENT_CONFIG_NOT_FOUND_ERROR_STR string = "deploymentconfigs.apps.openshift.io \"%s\" not found"
	DEPLOYMENT_CONFIG_NOT_FOUND           error  = fmt.Errorf("Requested deployment config does not exist")
)

// CreateArgs is a container of attributes of component create action
type CreateArgs struct {
	Name            string
	SourcePath      string
	SourceRef       string
	SourceType      config.SrcType
	ImageName       string
	EnvVars         []string
	Ports           []string
	Resources       *corev1.ResourceRequirements
	ApplicationName string
	Wait            bool
	// StorageToBeMounted describes the storage to be created
	// storagePath is the key of the map, the generatedPVC is the value of the map
	StorageToBeMounted map[string]*corev1.PersistentVolumeClaim
	StdOut             io.Writer
}

const (
	OcUpdateTimeout                 = 5 * time.Minute
	OpenShiftNameSpace              = "openshift"
	waitForComponentDeletionTimeout = 120 * time.Second

	// timeout for waiting for project deletion
	waitForProjectDeletionTimeOut = 3 * time.Minute

	// The length of the string to be generated for names of resources
	nameLength = 5

	// EnvS2IScriptsURL is an env var exposed to https://github.com/openshift/odo-init-image/blob/master/assemble-and-restart to indicate location of s2i scripts in this case assemble script
	EnvS2IScriptsURL = "ODO_S2I_SCRIPTS_URL"

	// EnvS2IScriptsProtocol is an env var exposed to https://github.com/openshift/odo-init-image/blob/master/assemble-and-restart to indicate the way to access location of s2i scripts indicated by ${${EnvS2IScriptsURL}} above
	EnvS2IScriptsProtocol = "ODO_S2I_SCRIPTS_PROTOCOL"

	// EnvS2ISrcOrBinPath is an env var exposed by s2i to indicate where the builder image expects the component source or binary to reside
	EnvS2ISrcOrBinPath = "ODO_S2I_SRC_BIN_PATH"

	// EnvS2ISrcBackupDir is the env var that points to the directory that holds a backup of component source
	// This is required bcoz, s2i assemble script moves(hence deletes contents) the contents of $ODO_S2I_SRC_BIN_PATH to $APP_ROOT during which $APP_DIR alo needs to be empty so that mv doesn't complain pushing to an already exisiting dir with same name
	EnvS2ISrcBackupDir = "ODO_SRC_BACKUP_DIR"

	// S2IScriptsURLLabel S2I script location Label name
	// Ref: https://docs.openshift.com/enterprise/3.2/creating_images/s2i.html#build-process
	S2IScriptsURLLabel = "io.openshift.s2i.scripts-url"

	// S2IBuilderImageName is the S2I builder image name
	S2IBuilderImageName = "name"

	// S2ISrcOrBinLabel is the label that provides, path where S2I expects component source or binary
	S2ISrcOrBinLabel = "io.openshift.s2i.destination"

	// EnvS2IBuilderImageName is the label that provides the name of builder image in component
	EnvS2IBuilderImageName = "ODO_S2I_BUILDER_IMG"

	// EnvS2IDeploymentDir is an env var exposed to https://github.com/openshift/odo-init-image/blob/master/assemble-and-restart to indicate s2i deployment directory
	EnvS2IDeploymentDir = "ODO_S2I_DEPLOYMENT_DIR"

	// DefaultS2ISrcOrBinPath is the default path where S2I expects source/binary artifacts in absence of $S2ISrcOrBinLabel in builder image
	// Ref: https://github.com/openshift/source-to-image/blob/master/docs/builder_image.md#required-image-contents
	DefaultS2ISrcOrBinPath = "/tmp"

	// DefaultS2ISrcBackupDir is the default path where odo backs up the component source
	DefaultS2ISrcBackupDir = "/opt/app-root/src-backup"

	// EnvS2IWorkingDir is an env var to odo-init-image assemble-and-restart.sh to indicate to it the s2i working directory
	EnvS2IWorkingDir = "ODO_S2I_WORKING_DIR"

	DefaultAppRootDir = "/opt/app-root"
)

// S2IPaths is a struct that will hold path to S2I scripts and the protocol indicating access to them, component source/binary paths, artifacts deployments directory
// These are passed as env vars to component pod
type S2IPaths struct {
	ScriptsPathProtocol string
	ScriptsPath         string
	SrcOrBinPath        string
	DeploymentDir       string
	WorkingDir          string
	SrcBackupPath       string
	BuilderImgName      string
}

// UpdateComponentParams serves the purpose of holding the arguments to a component update request
type UpdateComponentParams struct {
	// CommonObjectMeta is the object meta containing the labels and annotations expected for the new deployment
	CommonObjectMeta metav1.ObjectMeta
	// ResourceLimits are the cpu and memory constraints to be applied on to the component
	ResourceLimits corev1.ResourceRequirements
	// EnvVars to be exposed
	EnvVars []corev1.EnvVar
	// ExistingDC is the dc of the existing component that is requested for an update
	ExistingDC *appsv1.DeploymentConfig
	// DcRollOutWaitCond holds the logic to wait for dc with requested updates to be applied
	DcRollOutWaitCond dcRollOutWait
	// ImageMeta describes the image to be used in dc(builder image for local/binary and built component image for git deployments)
	ImageMeta CommonImageMeta
	// StorageToBeMounted describes the storage to be mounted
	// storagePath is the key of the map, the generatedPVC is the value of the map
	StorageToBeMounted map[string]*corev1.PersistentVolumeClaim
	// StorageToBeUnMounted describes the storage to be unmounted
	// path is the key of the map,storageName is the value of the map
	StorageToBeUnMounted map[string]string
}

// S2IDeploymentsDir is a set of possible S2I labels that provides S2I deployments directory
// This label is not uniform across different builder images. This slice is expected to grow as odo adds support to more component types and/or the respective builder images use different labels
var S2IDeploymentsDir = []string{
	"com.redhat.deployments-dir",
	"org.jboss.deployments-dir",
	"org.jboss.container.deployments-dir",
}

// errorMsg is the message for user when invalid configuration error occurs
const errorMsg = `
Please login to your server: 

odo login https://mycluster.mydomain.com
`

type Client struct {
	kubeClient           *kclient.Client
	imageClient          imageclientset.ImageV1Interface
	appsClient           appsclientset.AppsV1Interface
	buildClient          buildclientset.BuildV1Interface
	projectClient        projectclientset.ProjectV1Interface
	serviceCatalogClient servicecatalogclienset.ServicecatalogV1beta1Interface
	routeClient          routeclientset.RouteV1Interface
	userClient           userclientset.UserV1Interface
	KubeConfig           clientcmd.ClientConfig
	discoveryClient      discovery.DiscoveryInterface
	Namespace            string
	supportedResources   map[string]bool
}

func (c *Client) SetDiscoveryInterface(client discovery.DiscoveryInterface) {
	c.discoveryClient = client
}

// New creates a new client
func New() (*Client, error) {
	var client Client

	// initialize client-go clients
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{}
	client.KubeConfig = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)

	config, err := client.KubeConfig.ClientConfig()
	if err != nil {
		return nil, errors.New(err.Error() + errorMsg)
	}

	client.kubeClient, err = kclient.NewForConfig(client.KubeConfig)
	if err != nil {
		return nil, err
	}

	client.imageClient, err = imageclientset.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	client.appsClient, err = appsclientset.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	client.buildClient, err = buildclientset.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	client.serviceCatalogClient, err = servicecatalogclienset.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	client.projectClient, err = projectclientset.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	client.routeClient, err = routeclientset.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	client.userClient, err = userclientset.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	client.discoveryClient, err = discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		return nil, err
	}

	client.Namespace, _, err = client.KubeConfig.Namespace()
	if err != nil {
		return nil, err
	}

	return &client, nil
}

// ParseImageName parse image reference
// returns (imageNamespace, imageName, tag, digest, error)
// if image is referenced by tag (name:tag)  than digest is ""
// if image is referenced by digest (name@digest) than  tag is ""
func ParseImageName(image string) (string, string, string, string, error) {
	digestParts := strings.Split(image, "@")
	if len(digestParts) == 2 {
		// image is references digest
		// Safe path image name and digest are non empty, else error
		if digestParts[0] != "" && digestParts[1] != "" {
			// Image name might be fully qualified name of form: Namespace/ImageName
			imangeNameParts := strings.Split(digestParts[0], "/")
			if len(imangeNameParts) == 2 {
				return imangeNameParts[0], imangeNameParts[1], "", digestParts[1], nil
			}
			return "", imangeNameParts[0], "", digestParts[1], nil
		}
	} else if len(digestParts) == 1 && digestParts[0] != "" { // Filter out empty image name
		tagParts := strings.Split(image, ":")
		if len(tagParts) == 2 {
			// ":1.0.0 is invalid image name"
			if tagParts[0] != "" {
				// Image name might be fully qualified name of form: Namespace/ImageName
				imangeNameParts := strings.Split(tagParts[0], "/")
				if len(imangeNameParts) == 2 {
					return imangeNameParts[0], imangeNameParts[1], tagParts[1], "", nil
				}
				return "", tagParts[0], tagParts[1], "", nil
			}
		} else if len(tagParts) == 1 {
			// Image name might be fully qualified name of form: Namespace/ImageName
			imangeNameParts := strings.Split(tagParts[0], "/")
			if len(imangeNameParts) == 2 {
				return imangeNameParts[0], imangeNameParts[1], "latest", "", nil
			}
			return "", tagParts[0], "latest", "", nil
		}
	}
	return "", "", "", "", fmt.Errorf("invalid image reference %s", image)

}

// imageWithMetadata mutates the given image. It parses raw DockerImageManifest data stored in the image and
// fills its DockerImageMetadata and other fields.
// Copied from v3.7 github.com/openshift/origin/pkg/image/apis/image/v1/helpers.go
func imageWithMetadata(image *imagev1.Image) error {
	// Check if the metadata are already filled in for this image.
	meta, hasMetadata := image.DockerImageMetadata.Object.(*dockerapiv10.DockerImage)
	if hasMetadata && meta.Size > 0 {
		return nil
	}

	version := image.DockerImageMetadataVersion
	if len(version) == 0 {
		version = "1.0"
	}

	obj := &dockerapiv10.DockerImage{}
	if len(image.DockerImageMetadata.Raw) != 0 {
		if err := json.Unmarshal(image.DockerImageMetadata.Raw, obj); err != nil {
			return err
		}
		image.DockerImageMetadata.Object = obj
	}

	image.DockerImageMetadataVersion = version

	return nil
}

// GetPortsFromBuilderImage returns list of available port from given builder image of given component type
func (c *Client) GetPortsFromBuilderImage(componentType string) ([]string, error) {
	// checking port through builder image
	imageNS, imageName, imageTag, _, err := ParseImageName(componentType)
	if err != nil {
		return []string{}, err
	}
	imageStream, err := c.GetImageStream(imageNS, imageName, imageTag)
	if err != nil {
		return []string{}, err
	}
	imageStreamImage, err := c.GetImageStreamImage(imageStream, imageTag)
	if err != nil {
		return []string{}, err
	}
	containerPorts, err := c.GetExposedPorts(imageStreamImage)
	if err != nil {
		return []string{}, err
	}
	var portList []string
	for _, po := range containerPorts {
		port := fmt.Sprint(po.ContainerPort) + "/" + string(po.Protocol)
		portList = append(portList, port)
	}
	if len(portList) == 0 {
		return []string{}, fmt.Errorf("given component type doesn't expose any ports, please use --port flag to specify a port")
	}
	return portList, nil
}

// RunLogout logs out the current user from cluster
func (c *Client) RunLogout(stdout io.Writer) error {
	output, err := c.userClient.Users().Get("~", metav1.GetOptions{})
	if err != nil {
		klog.V(1).Infof("%v : unable to get userinfo", err)
	}

	// read the current config form ~/.kube/config
	conf, err := c.KubeConfig.ClientConfig()
	if err != nil {
		klog.V(1).Infof("%v : unable to get client config", err)
	}
	// initialising oauthv1client
	client, err := oauthv1client.NewForConfig(conf)
	if err != nil {
		klog.V(1).Infof("%v : unable to create a new OauthV1Client", err)
	}

	// deleting token form the server
	if err := client.OAuthAccessTokens().Delete(conf.BearerToken, &metav1.DeleteOptions{}); err != nil {
		klog.V(1).Infof("%v", err)
	}

	rawConfig, err := c.KubeConfig.RawConfig()
	if err != nil {
		klog.V(1).Infof("%v : unable to switch to  project", err)
	}

	// deleting token for the current server from local config
	for key, value := range rawConfig.AuthInfos {
		if key == rawConfig.Contexts[rawConfig.CurrentContext].AuthInfo {
			value.Token = ""
		}
	}
	err = clientcmd.ModifyConfig(clientcmd.NewDefaultClientConfigLoadingRules(), rawConfig, true)
	if err != nil {
		klog.V(1).Infof("%v : unable to write config to config file", err)
	}

	_, err = io.WriteString(stdout, fmt.Sprintf("Logged \"%v\" out on \"%v\"\n", output.Name, conf.Host))
	return err
}

// isServerUp returns true if server is up and running
// server parameter has to be a valid url
func isServerUp(server string) bool {
	// initialising the default timeout, this will be used
	// when the value is not readable from config
	ocRequestTimeout := preference.DefaultTimeout * time.Second
	// checking the value of timeout in config
	// before proceeding with default timeout
	cfg, configReadErr := preference.New()
	if configReadErr != nil {
		klog.V(3).Info(errors.Wrap(configReadErr, "unable to read config file"))
	} else {
		ocRequestTimeout = time.Duration(cfg.GetTimeout()) * time.Second
	}
	address, err := util.GetHostWithPort(server)
	if err != nil {
		klog.V(3).Infof("Unable to parse url %s (%s)", server, err)
	}
	klog.V(3).Infof("Trying to connect to server %s", address)
	_, connectionError := net.DialTimeout("tcp", address, time.Duration(ocRequestTimeout))
	if connectionError != nil {
		klog.V(3).Info(errors.Wrap(connectionError, "unable to connect to server"))
		return false
	}

	klog.V(3).Infof("Server %v is up", server)
	return true
}

// addLabelsToArgs adds labels from map to args as a new argument in format that oc requires
// --labels label1=value1,label2=value2
func addLabelsToArgs(labels map[string]string, args []string) []string {
	if labels != nil {
		var labelsString []string
		for key, value := range labels {
			labelsString = append(labelsString, fmt.Sprintf("%s=%s", key, value))
		}
		args = append(args, "--labels")
		args = append(args, strings.Join(labelsString, ","))
	}

	return args
}

// getExposedPortsFromISI parse ImageStreamImage definition and return all exposed ports in form of ContainerPorts structs
func getExposedPortsFromISI(image *imagev1.ImageStreamImage) ([]corev1.ContainerPort, error) {
	// file DockerImageMetadata
	err := imageWithMetadata(&image.Image)
	if err != nil {
		return nil, err
	}

	var ports []corev1.ContainerPort

	var exposedPorts = image.Image.DockerImageMetadata.Object.(*dockerapiv10.DockerImage).ContainerConfig.ExposedPorts

	if image.Image.DockerImageMetadata.Object.(*dockerapiv10.DockerImage).Config != nil {
		if exposedPorts == nil {
			exposedPorts = make(map[string]struct{})
		}

		// add ports from Config
		for exposedPort := range image.Image.DockerImageMetadata.Object.(*dockerapiv10.DockerImage).Config.ExposedPorts {
			var emptyStruct struct{}
			exposedPorts[exposedPort] = emptyStruct
		}
	}

	for exposedPort := range exposedPorts {
		splits := strings.Split(exposedPort, "/")
		if len(splits) != 2 {
			return nil, fmt.Errorf("invalid port %s", exposedPort)
		}

		portNumberI64, err := strconv.ParseInt(splits[0], 10, 32)
		if err != nil {
			return nil, errors.Wrapf(err, "invalid port number %s", splits[0])
		}
		portNumber := int32(portNumberI64)

		var portProto corev1.Protocol
		switch strings.ToUpper(splits[1]) {
		case "TCP":
			portProto = corev1.ProtocolTCP
		case "UDP":
			portProto = corev1.ProtocolUDP
		default:
			return nil, fmt.Errorf("invalid port protocol %s", splits[1])
		}

		port := corev1.ContainerPort{
			Name:          fmt.Sprintf("%d-%s", portNumber, strings.ToLower(string(portProto))),
			ContainerPort: portNumber,
			Protocol:      portProto,
		}

		ports = append(ports, port)
	}

	return ports, nil
}

// GetImageStreams returns the Image Stream objects in the given namespace
func (c *Client) GetImageStreams(namespace string) ([]imagev1.ImageStream, error) {
	imageStreamList, err := c.imageClient.ImageStreams(namespace).List(metav1.ListOptions{})
	if err != nil {
		return nil, errors.Wrap(err, "unable to list imagestreams")
	}
	return imageStreamList.Items, nil
}

// GetImageStreamsNames returns the names of the image streams in a given
// namespace
func (c *Client) GetImageStreamsNames(namespace string) ([]string, error) {
	imageStreams, err := c.GetImageStreams(namespace)
	if err != nil {
		return nil, errors.Wrap(err, "unable to get image streams")
	}

	var names []string
	for _, imageStream := range imageStreams {
		names = append(names, imageStream.Name)
	}
	return names, nil
}

// isTagInImageStream takes a imagestream and a tag and checks if the tag is present in the imagestream's status attribute
func isTagInImageStream(is imagev1.ImageStream, imageTag string) bool {
	// Loop through the tags in the imagestream's status attribute
	for _, tag := range is.Status.Tags {
		// look for a matching tag
		if tag.Tag == imageTag {
			// Return true if found
			return true
		}
	}
	// Return false if not found.
	return false
}

// GetImageStream returns the imagestream using image details like imageNS, imageName and imageTag
// imageNS can be empty in which case, this function searches currentNamespace on priority. If
// imagestream of required tag not found in current namespace, then searches openshift namespace.
// If not found, error out. If imageNS is not empty string, then, the requested imageNS only is searched
// for requested imagestream
func (c *Client) GetImageStream(imageNS string, imageName string, imageTag string) (*imagev1.ImageStream, error) {
	var err error
	var imageStream *imagev1.ImageStream
	currentProjectName := c.GetCurrentProjectName()
	/*
		If User has not chosen image NS then,
			1. Use image from current NS if available
			2. If not 1, use default openshift NS
			3. If not 2, return errors from both 1 and 2
		else
			Use user chosen namespace
			If image doesn't exist in user chosen namespace,
				error out
			else
				Proceed
	*/
	// User has not passed any particular ImageStream
	if imageNS == "" {

		// First try finding imagestream from current namespace
		currentNSImageStream, e := c.imageClient.ImageStreams(currentProjectName).Get(imageName, metav1.GetOptions{})
		if e != nil {
			err = errors.Wrapf(e, "no match found for : %s in namespace %s", imageName, currentProjectName)
		} else {
			if isTagInImageStream(*currentNSImageStream, imageTag) {
				return currentNSImageStream, nil
			}
		}

		// If not in current namespace, try finding imagestream from openshift namespace
		openshiftNSImageStream, e := c.imageClient.ImageStreams(OpenShiftNameSpace).Get(imageName, metav1.GetOptions{})
		if e != nil {
			// The image is not available in current Namespace.
			err = errors.Wrapf(e, "no match found for : %s in namespace %s", imageName, OpenShiftNameSpace)
		} else {
			if isTagInImageStream(*openshiftNSImageStream, imageTag) {
				return openshiftNSImageStream, nil
			}
		}
		if e != nil && err != nil {
			return nil, fmt.Errorf("component type %q not found", imageName)
		}

		// Required tag not in openshift and current namespaces
		return nil, fmt.Errorf("image stream %s with tag %s not found in openshift and %s namespaces", imageName, imageTag, currentProjectName)

	}

	// Fetch imagestream from requested namespace
	imageStream, err = c.imageClient.ImageStreams(imageNS).Get(imageName, metav1.GetOptions{})
	if err != nil {
		return nil, errors.Wrapf(
			err, "no match found for %s in namespace %s", imageName, imageNS,
		)
	}
	if !isTagInImageStream(*imageStream, imageTag) {
		return nil, fmt.Errorf("image stream %s with tag %s not found in %s namespaces", imageName, imageTag, currentProjectName)
	}

	return imageStream, nil
}

// GetImageStreamImage returns image and error if any, corresponding to the passed imagestream and image tag
func (c *Client) GetImageStreamImage(imageStream *imagev1.ImageStream, imageTag string) (*imagev1.ImageStreamImage, error) {
	imageNS := imageStream.ObjectMeta.Namespace
	imageName := imageStream.ObjectMeta.Name

	for _, tag := range imageStream.Status.Tags {
		// look for matching tag
		if tag.Tag == imageTag {
			klog.V(3).Infof("Found exact image tag match for %s:%s", imageName, imageTag)

			if len(tag.Items) > 0 {
				tagDigest := tag.Items[0].Image
				imageStreamImageName := fmt.Sprintf("%s@%s", imageName, tagDigest)

				// look for imageStreamImage for given tag (reference by digest)
				imageStreamImage, err := c.imageClient.ImageStreamImages(imageNS).Get(imageStreamImageName, metav1.GetOptions{})
				if err != nil {
					return nil, errors.Wrapf(err, "unable to find ImageStreamImage with  %s digest", imageStreamImageName)
				}
				return imageStreamImage, nil
			}

			return nil, fmt.Errorf("unable to find tag %s for image %s", imageTag, imageName)

		}
	}

	// return error since its an unhandled case if code reaches here
	return nil, fmt.Errorf("unable to find tag %s for image %s", imageTag, imageName)
}

// GetImageStreamTags returns all the ImageStreamTag objects in the given namespace
func (c *Client) GetImageStreamTags(namespace string) ([]imagev1.ImageStreamTag, error) {
	imageStreamTagList, err := c.imageClient.ImageStreamTags(namespace).List(metav1.ListOptions{})
	if err != nil {
		return nil, errors.Wrap(err, "unable to list imagestreamtags")
	}
	return imageStreamTagList.Items, nil
}

// GetExposedPorts returns list of ContainerPorts that are exposed by given image
func (c *Client) GetExposedPorts(imageStreamImage *imagev1.ImageStreamImage) ([]corev1.ContainerPort, error) {
	var containerPorts []corev1.ContainerPort

	// get ports that are exported by image
	containerPorts, err := getExposedPortsFromISI(imageStreamImage)
	if err != nil {
		return nil, errors.Wrapf(err, "unable to get exported ports from image %+v", imageStreamImage)
	}

	return containerPorts, nil
}

func getAppRootVolumeName(dcName string) string {
	return fmt.Sprintf("%s-s2idata", dcName)
}

// NewAppS2I is only used with "Git" as we need Build
// gitURL is the url of the git repo
// inputPorts is the array containing the string port values
// envVars is the array containing the string env var values
func (c *Client) NewAppS2I(params CreateArgs, commonObjectMeta metav1.ObjectMeta) error {
	klog.V(3).Infof("Using BuilderImage: %s", params.ImageName)
	imageNS, imageName, imageTag, _, err := ParseImageName(params.ImageName)
	if err != nil {
		return errors.Wrap(err, "unable to parse image name")
	}
	imageStream, err := c.GetImageStream(imageNS, imageName, imageTag)
	if err != nil {
		return errors.Wrap(err, "unable to retrieve ImageStream for NewAppS2I")
	}
	/*
	 Set imageNS to the commonObjectMeta.Namespace of above fetched imagestream because, the commonObjectMeta.Namespace passed here can potentially be emptystring
	 in which case, GetImageStream function resolves to correct commonObjectMeta.Namespace in accordance with priorities in GetImageStream
	*/

	imageNS = imageStream.ObjectMeta.Namespace
	klog.V(3).Infof("Using imageNS: %s", imageNS)

	imageStreamImage, err := c.GetImageStreamImage(imageStream, imageTag)
	if err != nil {
		return errors.Wrapf(err, "unable to create s2i app for %s", commonObjectMeta.Name)
	}

	var containerPorts []corev1.ContainerPort
	if len(params.Ports) == 0 {
		containerPorts, err = c.GetExposedPorts(imageStreamImage)
		if err != nil {
			return errors.Wrapf(err, "unable to get exposed ports for %s:%s", imageName, imageTag)
		}
	} else {
		if err != nil {
			return errors.Wrapf(err, "unable to create s2i app for %s", commonObjectMeta.Name)
		}
		containerPorts, err = util.GetContainerPortsFromStrings(params.Ports)
		if err != nil {
			return errors.Wrapf(err, "unable to get container ports from %v", params.Ports)
		}
	}

	inputEnvVars, err := GetInputEnvVarsFromStrings(params.EnvVars)
	if err != nil {
		return errors.Wrapf(err, "error adding environment variables to the container")
	}

	// generate and create ImageStream
	is := imagev1.ImageStream{
		ObjectMeta: commonObjectMeta,
	}
	_, err = c.imageClient.ImageStreams(c.Namespace).Create(&is)
	if err != nil {
		return errors.Wrapf(err, "unable to create ImageStream for %s", commonObjectMeta.Name)
	}

	// if gitURL is not set, error out
	if params.SourcePath == "" {
		return errors.New("unable to create buildSource with empty gitURL")
	}

	// Deploy BuildConfig to build the container with Git
	buildConfig, err := c.CreateBuildConfig(commonObjectMeta, params.ImageName, params.SourcePath, params.SourceRef, inputEnvVars)
	if err != nil {
		return errors.Wrapf(err, "unable to deploy BuildConfig for %s", commonObjectMeta.Name)
	}

	// Generate and create the DeploymentConfig
	dc := generateGitDeploymentConfig(commonObjectMeta, buildConfig.Spec.Output.To.Name, containerPorts, inputEnvVars, params.Resources)
	err = addOrRemoveVolumeAndVolumeMount(c, &dc, params.StorageToBeMounted, nil)
	if err != nil {
		return errors.Wrapf(err, "failed to mount and unmount pvc to dc")
	}
	createdDC, err := c.appsClient.DeploymentConfigs(c.Namespace).Create(&dc)
	if err != nil {
		return errors.Wrapf(err, "unable to create DeploymentConfig for %s", commonObjectMeta.Name)
	}

	ownerReference := GenerateOwnerReference(createdDC)

	// update the owner references for the new storage
	for _, storage := range params.StorageToBeMounted {
		err := updateStorageOwnerReference(c, storage, ownerReference)
		if err != nil {
			return errors.Wrapf(err, "unable to update owner reference of storage")
		}
	}

	// Create a service
	commonObjectMeta.SetOwnerReferences(append(commonObjectMeta.GetOwnerReferences(), ownerReference))
	svc, err := c.GetKubeClient().CreateService(commonObjectMeta, generateServiceSpec(commonObjectMeta, dc.Spec.Template.Spec.Containers[0].Ports))
	if err != nil {
		return errors.Wrapf(err, "unable to create Service for %s", commonObjectMeta.Name)
	}

	// Create secret(s)
	err = c.GetKubeClient().CreateSecrets(params.Name, commonObjectMeta, svc, ownerReference)

	return err
}

// getS2ILabelValue returns the requested S2I label value from the passed set of labels attached to builder image
// and the hard coded possible list(the labels are not uniform across different builder images) of expected labels
func getS2ILabelValue(labels map[string]string, expectedLabelsSet []string) string {
	for _, label := range expectedLabelsSet {
		if retVal, ok := labels[label]; ok {
			return retVal
		}
	}
	return ""
}

// GetS2IMetaInfoFromBuilderImg returns script path protocol, S2I scripts path, S2I source or binary expected path, S2I deployment dir and errors(if any) from the passed builder image
func GetS2IMetaInfoFromBuilderImg(builderImage *imagev1.ImageStreamImage) (S2IPaths, error) {

	// Define structs for internal un-marshalling of imagestreamimage to extract label from it
	type ContainerConfig struct {
		Labels     map[string]string `json:"Labels"`
		WorkingDir string            `json:"WorkingDir"`
	}
	type DockerImageMetaDataRaw struct {
		ContainerConfig ContainerConfig `json:"ContainerConfig"`
		Config          ContainerConfig `json:"Config"`
	}

	var dimdr DockerImageMetaDataRaw

	// The label $S2IScriptsURLLabel needs to be extracted from builderImage#Image#DockerImageMetadata#Raw which is byte array
	dimdrByteArr := (*builderImage).Image.DockerImageMetadata.Raw

	// Unmarshal the byte array into the struct for ease of access of required fields
	err := json.Unmarshal(dimdrByteArr, &dimdr)
	if err != nil {
		return S2IPaths{}, errors.Wrap(err, "unable to bootstrap supervisord")
	}

	labels := make(map[string]string)

	// Put labels from both ContainerConfig and Config into one map
	// if there is the same label in both, the Config has priority
	for k, v := range dimdr.ContainerConfig.Labels {
		labels[k] = v
	}
	for k, v := range dimdr.Config.Labels {
		labels[k] = v
	}

	// If by any chance, labels attribute is nil(although ideally not the case for builder images), return
	if len(labels) == 0 {
		klog.V(3).Infof("No Labels found in %+v in builder image %+v", dimdr, builderImage)
		return S2IPaths{}, nil
	}

	// Extract the label containing S2I scripts URL
	s2iScriptsURL := labels[S2IScriptsURLLabel]
	s2iSrcOrBinPath := labels[S2ISrcOrBinLabel]
	s2iBuilderImgName := labels[S2IBuilderImageName]

	if s2iSrcOrBinPath == "" {
		// In cases like nodejs builder image, where there is no concept of binary and sources are directly run, use destination as source
		// s2iSrcOrBinPath = getS2ILabelValue(dimdr.Config.Labels, S2IDeploymentsDir)
		s2iSrcOrBinPath = DefaultS2ISrcOrBinPath
	}

	s2iDestinationDir := getS2ILabelValue(labels, S2IDeploymentsDir)
	// The URL is a combination of protocol and the path to script details of which can be found @
	// https://github.com/openshift/source-to-image/blob/master/docs/builder_image.md#s2i-scripts
	// Extract them out into protocol and path separately to minimise the task in
	// https://github.com/openshift/odo-init-image/blob/master/assemble-and-restart when custom handling
	// for each of the protocols is added
	s2iScriptsProtocol := ""
	s2iScriptsPath := ""

	switch {
	case strings.HasPrefix(s2iScriptsURL, "image://"):
		s2iScriptsProtocol = "image://"
		s2iScriptsPath = strings.TrimPrefix(s2iScriptsURL, "image://")
	case strings.HasPrefix(s2iScriptsURL, "file://"):
		s2iScriptsProtocol = "file://"
		s2iScriptsPath = strings.TrimPrefix(s2iScriptsURL, "file://")
	case strings.HasPrefix(s2iScriptsURL, "http(s)://"):
		s2iScriptsProtocol = "http(s)://"
		s2iScriptsPath = s2iScriptsURL
	default:
		return S2IPaths{}, fmt.Errorf("Unknown scripts url %s", s2iScriptsURL)
	}
	return S2IPaths{
		ScriptsPathProtocol: s2iScriptsProtocol,
		ScriptsPath:         s2iScriptsPath,
		SrcOrBinPath:        s2iSrcOrBinPath,
		DeploymentDir:       s2iDestinationDir,
		WorkingDir:          dimdr.Config.WorkingDir,
		SrcBackupPath:       DefaultS2ISrcBackupDir,
		BuilderImgName:      s2iBuilderImgName,
	}, nil
}

// uniqueAppendOrOverwriteEnvVars appends/overwrites the passed existing list of env vars with the elements from the to-be appended passed list of envs
func uniqueAppendOrOverwriteEnvVars(existingEnvs []corev1.EnvVar, envVars ...corev1.EnvVar) []corev1.EnvVar {
	mapExistingEnvs := make(map[string]corev1.EnvVar)
	var retVal []corev1.EnvVar

	// Convert slice of existing env vars to map to check for existence
	for _, envVar := range existingEnvs {
		mapExistingEnvs[envVar.Name] = envVar
	}

	// For each new envVar to be appended, Add(if envVar with same name doesn't already exist) / overwrite(if envVar with same name already exists) the map
	for _, newEnvVar := range envVars {
		mapExistingEnvs[newEnvVar.Name] = newEnvVar
	}

	// append the values to the final slice
	// don't loop because we need them in order
	for _, envVar := range existingEnvs {
		if val, ok := mapExistingEnvs[envVar.Name]; ok {
			retVal = append(retVal, val)
			delete(mapExistingEnvs, envVar.Name)
		}
	}

	for _, newEnvVar := range envVars {
		if val, ok := mapExistingEnvs[newEnvVar.Name]; ok {
			retVal = append(retVal, val)
		}
	}

	return retVal
}

// deleteEnvVars deletes the passed env var from the list of passed env vars
// Parameters:
//	existingEnvs: Slice of existing env vars
//	envTobeDeleted: The name of env var to be deleted
// Returns:
//	slice of env vars with delete reflected
func deleteEnvVars(existingEnvs []corev1.EnvVar, envTobeDeleted string) []corev1.EnvVar {
	retVal := make([]corev1.EnvVar, len(existingEnvs))
	copy(retVal, existingEnvs)
	for ind, envVar := range retVal {
		if envVar.Name == envTobeDeleted {
			retVal = append(retVal[:ind], retVal[ind+1:]...)
			break
		}
	}
	return retVal
}

// BootstrapSupervisoredS2I uses S2I (Source To Image) to inject Supervisor into the application container.
// Odo uses https://github.com/ochinchina/supervisord which is pre-built in a ready-to-deploy InitContainer.
// The supervisord binary is copied over to the application container using a temporary volume and overrides
// the built-in S2I run function for the supervisord run command instead.
//
// Supervisor keeps the pod running (as PID 1), so you it is possible to trigger assembly script inside running pod,
// and than restart application using Supervisor without need to restart the container/Pod.
//
func (c *Client) BootstrapSupervisoredS2I(params CreateArgs, commonObjectMeta metav1.ObjectMeta) error {
	imageNS, imageName, imageTag, _, err := ParseImageName(params.ImageName)

	if err != nil {
		return errors.Wrap(err, "unable to create new s2i git build ")
	}
	imageStream, err := c.GetImageStream(imageNS, imageName, imageTag)
	if err != nil {
		return errors.Wrap(err, "Failed to bootstrap supervisored")
	}
	/*
	 Set imageNS to the commonObjectMeta.Namespace of above fetched imagestream because, the commonObjectMeta.Namespace passed here can potentially be emptystring
	 in which case, GetImageStream function resolves to correct commonObjectMeta.Namespace in accordance with priorities in GetImageStream
	*/
	imageNS = imageStream.ObjectMeta.Namespace

	imageStreamImage, err := c.GetImageStreamImage(imageStream, imageTag)
	if err != nil {
		return errors.Wrap(err, "unable to bootstrap supervisord")
	}
	var containerPorts []corev1.ContainerPort
	containerPorts, err = util.GetContainerPortsFromStrings(params.Ports)
	if err != nil {
		return errors.Wrapf(err, "unable to get container ports from %v", params.Ports)
	}

	inputEnvs, err := GetInputEnvVarsFromStrings(params.EnvVars)
	if err != nil {
		return errors.Wrapf(err, "error adding environment variables to the container")
	}

	// generate and create ImageStream
	is := imagev1.ImageStream{
		ObjectMeta: commonObjectMeta,
	}
	_, err = c.imageClient.ImageStreams(c.Namespace).Create(&is)
	if err != nil {
		return errors.Wrapf(err, "unable to create ImageStream for %s", commonObjectMeta.Name)
	}

	commonImageMeta := CommonImageMeta{
		Name:      imageName,
		Tag:       imageTag,
		Namespace: imageNS,
		Ports:     containerPorts,
	}

	// Extract s2i scripts path and path type from imagestream image
	//s2iScriptsProtocol, s2iScriptsURL, s2iSrcOrBinPath, s2iDestinationDir
	s2iPaths, err := GetS2IMetaInfoFromBuilderImg(imageStreamImage)
	if err != nil {
		return errors.Wrap(err, "unable to bootstrap supervisord")
	}

	// Append s2i related parameters extracted above to env
	inputEnvs = injectS2IPaths(inputEnvs, s2iPaths)

	if params.SourceType == config.LOCAL {
		inputEnvs = uniqueAppendOrOverwriteEnvVars(
			inputEnvs,
			corev1.EnvVar{
				Name:  EnvS2ISrcBackupDir,
				Value: s2iPaths.SrcBackupPath,
			},
		)
	}

	// Generate the DeploymentConfig that will be used.
	dc := generateSupervisordDeploymentConfig(
		commonObjectMeta,
		commonImageMeta,
		inputEnvs,
		[]corev1.EnvFromSource{},
		params.Resources,
	)
	if len(inputEnvs) != 0 {
		err = updateEnvVar(&dc, inputEnvs)
		if err != nil {
			return errors.Wrapf(err, "unable to add env vars to the container")
		}
	}

	addInitVolumesToDC(&dc, commonObjectMeta.Name, s2iPaths.DeploymentDir)

	err = addOrRemoveVolumeAndVolumeMount(c, &dc, params.StorageToBeMounted, nil)
	if err != nil {
		return errors.Wrapf(err, "failed to mount and unmount pvc to dc")
	}

	createdDC, err := c.appsClient.DeploymentConfigs(c.Namespace).Create(&dc)
	if err != nil {
		return errors.Wrapf(err, "unable to create DeploymentConfig for %s", commonObjectMeta.Name)
	}

	var jsonDC []byte
	jsonDC, _ = json.Marshal(createdDC)
	klog.V(5).Infof("Created new DeploymentConfig:\n%s\n", string(jsonDC))

	ownerReference := GenerateOwnerReference(createdDC)

	// update the owner references for the new storage
	for _, storage := range params.StorageToBeMounted {
		err := updateStorageOwnerReference(c, storage, ownerReference)
		if err != nil {
			return errors.Wrapf(err, "unable to update owner reference of storage")
		}
	}

	// Create a service
	commonObjectMeta.SetOwnerReferences(append(commonObjectMeta.GetOwnerReferences(), ownerReference))

	svc, err := c.GetKubeClient().CreateService(commonObjectMeta, generateServiceSpec(commonObjectMeta, dc.Spec.Template.Spec.Containers[0].Ports))
	if err != nil {
		return errors.Wrapf(err, "unable to create Service for %s", commonObjectMeta.Name)
	}

	err = c.GetKubeClient().CreateSecrets(params.Name, commonObjectMeta, svc, ownerReference)
	if err != nil {
		return err
	}

	// Setup PVC.
	_, err = c.CreatePVC(getAppRootVolumeName(commonObjectMeta.Name), "1Gi", commonObjectMeta.Labels, ownerReference)
	if err != nil {
		return errors.Wrapf(err, "unable to create PVC for %s", commonObjectMeta.Name)
	}

	return nil
}

// updateEnvVar updates the environmental variables to the container in the DC
// dc is the deployment config to be updated
// envVars is the array containing the corev1.EnvVar values
func updateEnvVar(dc *appsv1.DeploymentConfig, envVars []corev1.EnvVar) error {
	numContainers := len(dc.Spec.Template.Spec.Containers)
	if numContainers != 1 {
		return fmt.Errorf("expected exactly one container in Deployment Config %v, got %v", dc.Name, numContainers)
	}

	dc.Spec.Template.Spec.Containers[0].Env = envVars
	return nil
}

// UpdateBuildConfig updates the BuildConfig file
// buildConfigName is the name of the BuildConfig file to be updated
// projectName is the name of the project
// gitURL equals to the git URL of the source and is equals to "" if the source is of type dir or binary
// annotations contains the annotations for the BuildConfig file
func (c *Client) UpdateBuildConfig(buildConfigName string, gitURL string, annotations map[string]string) error {
	if gitURL == "" {
		return errors.New("gitURL for UpdateBuildConfig must not be blank")
	}

	// generate BuildConfig
	buildSource := buildv1.BuildSource{
		Git: &buildv1.GitBuildSource{
			URI: gitURL,
		},
		Type: buildv1.BuildSourceGit,
	}

	buildConfig, err := c.GetBuildConfigFromName(buildConfigName)
	if err != nil {
		return errors.Wrap(err, "unable to get the BuildConfig file")
	}
	buildConfig.Spec.Source = buildSource
	buildConfig.Annotations = annotations
	_, err = c.buildClient.BuildConfigs(c.Namespace).Update(buildConfig)
	if err != nil {
		return errors.Wrap(err, "unable to update the component")
	}
	return nil
}

// Define a function that is meant to update a DC in place
type dcStructUpdater func(dc *appsv1.DeploymentConfig) error
type dcRollOutWait func(*appsv1.DeploymentConfig, int64) bool

// PatchCurrentDC "patches" the current DeploymentConfig with a new one
// however... we make sure that configurations such as:
// - volumes
// - environment variables
// are correctly copied over / consistent without an issue.
// if prePatchDCHandler is specified (meaning not nil), then it's applied
// as the last action before the actual call to the Kubernetes API thus giving us the chance
// to perform arbitrary updates to a DC before it's finalized for patching
// isGit indicates if the deployment config belongs to a git component or a local/binary component
func (c *Client) PatchCurrentDC(dc appsv1.DeploymentConfig, prePatchDCHandler dcStructUpdater, existingCmpContainer corev1.Container, ucp UpdateComponentParams, isGit bool) error {

	name := ucp.CommonObjectMeta.Name
	currentDC := ucp.ExistingDC
	modifiedDC := *currentDC

	waitCond := ucp.DcRollOutWaitCond

	// copy the any remaining volumes and volume mounts
	copyVolumesAndVolumeMounts(dc, currentDC, existingCmpContainer)

	if prePatchDCHandler != nil {
		err := prePatchDCHandler(&dc)
		if err != nil {
			return errors.Wrapf(err, "Unable to correctly update dc %s using the specified prePatch handler", name)
		}
	}

	// now mount/unmount the newly created/deleted pvc
	err := addOrRemoveVolumeAndVolumeMount(c, &dc, ucp.StorageToBeMounted, ucp.StorageToBeUnMounted)
	if err != nil {
		return err
	}

	// Replace the current spec with the new one
	modifiedDC.Spec = dc.Spec

	// Replace the old annotations with the new ones too
	// the reason we do this is because Kubernetes handles metadata such as resourceVersion
	// that should not be overridden.
	modifiedDC.ObjectMeta.Annotations = dc.ObjectMeta.Annotations
	modifiedDC.ObjectMeta.Labels = dc.ObjectMeta.Labels

	// Update the current one that's deployed with the new Spec.
	// despite the "patch" function name, we use update since `.Patch` requires
	// use to define each and every object we must change. Updating makes it easier.
	updatedDc, err := c.appsClient.DeploymentConfigs(c.Namespace).Update(&modifiedDC)

	if err != nil {
		return errors.Wrapf(err, "unable to update DeploymentConfig %s", name)
	}

	// if isGit is true, the DC belongs to a git component
	// since build happens for every push in case of git and a new image is pushed, we need to wait
	// so git oriented deployments, we start the deployment before waiting for it to be updated
	if isGit {
		_, err := c.StartDeployment(updatedDc.Name)
		if err != nil {
			return errors.Wrapf(err, "unable to start deployment")
		}
	} else {
		// not a git oriented deployment, check before waiting
		// we check after the update that the template in the earlier and the new dc are same or not
		// if they are same, we don't wait as new deployment won't run and we will wait till timeout
		// inspired from https://github.com/openshift/origin/blob/bb1b9b5223dd37e63790d99095eec04bfd52b848/pkg/apps/controller/deploymentconfig/deploymentconfig_controller.go#L609
		if reflect.DeepEqual(updatedDc.Spec.Template, currentDC.Spec.Template) {
			return nil
		}
		currentDCBytes, errCurrent := json.Marshal(currentDC.Spec.Template)
		updatedDCBytes, errUpdated := json.Marshal(updatedDc.Spec.Template)
		if errCurrent != nil || errUpdated != nil {
			return errors.Wrapf(err, "unable to unmarshal dc")
		}
		klog.V(3).Infof("going to wait for new deployment roll out because updatedDc Spec.Template: %v doesn't match currentDc Spec.Template: %v", string(updatedDCBytes), string(currentDCBytes))

	}

	// We use the currentDC + 1 for the next revision.. We do NOT use the updated DC (see above code)
	// as the "Update" function will not update the Status.LatestVersion quick enough... so we wait until
	// the current revision + 1 is available.
	desiredRevision := currentDC.Status.LatestVersion + 1

	// Watch / wait for deploymentconfig to update annotations
	// importing "component" results in an import loop, so we do *not* use the constants here.
	_, err = c.WaitAndGetDC(name, desiredRevision, OcUpdateTimeout, waitCond)
	if err != nil {
		return errors.Wrapf(err, "unable to wait for DeploymentConfig %s to update", name)
	}

	// update the owner references for the new storage
	for _, storage := range ucp.StorageToBeMounted {
		err := updateStorageOwnerReference(c, storage, GenerateOwnerReference(updatedDc))
		if err != nil {
			return errors.Wrapf(err, "unable to update owner reference of storage")
		}
	}

	return nil
}

// copies volumes and volume mounts from currentDC to dc, excluding the supervisord related ones
func copyVolumesAndVolumeMounts(dc appsv1.DeploymentConfig, currentDC *appsv1.DeploymentConfig, matchingContainer corev1.Container) {
	// Append the existing VolumeMounts to the new DC. We use "range" and find the correct container rather than
	// using .spec.Containers[0] *in case* the template ever changes and a new container has been added.
	for index, container := range dc.Spec.Template.Spec.Containers {
		// Find the container
		if container.Name == matchingContainer.Name {

			// create a map of volume mount names for faster searching later
			dcVolumeMountsMap := make(map[string]bool)
			for _, volumeMount := range container.VolumeMounts {
				dcVolumeMountsMap[volumeMount.Name] = true
			}

			// Loop through all the volumes
			for _, volume := range matchingContainer.VolumeMounts {
				// If it's the supervisord volume, ignore it.
				if volume.Name == common.SupervisordVolumeName {
					continue
				} else {
					// check if we are appending the same volume mount again or not
					if _, ok := dcVolumeMountsMap[volume.Name]; !ok {
						dc.Spec.Template.Spec.Containers[index].VolumeMounts = append(dc.Spec.Template.Spec.Containers[index].VolumeMounts, volume)
					}
				}
			}

			// Break out since we've succeeded in updating the container we were looking for
			break
		}
	}

	// create a map of volume names for faster searching later
	dcVolumeMap := make(map[string]bool)
	for _, volume := range dc.Spec.Template.Spec.Volumes {
		dcVolumeMap[volume.Name] = true
	}

	// Now the same with Volumes, again, ignoring the supervisord volume.
	for _, volume := range currentDC.Spec.Template.Spec.Volumes {
		if volume.Name == common.SupervisordVolumeName {
			continue
		} else {
			// check if we are appending the same volume again or not
			if _, ok := dcVolumeMap[volume.Name]; !ok {
				dc.Spec.Template.Spec.Volumes = append(dc.Spec.Template.Spec.Volumes, volume)
			}
		}
	}
}

// UpdateDCToGit replaces / updates the current DeplomentConfig with the appropriate
// generated image from BuildConfig as well as the correct DeploymentConfig triggers for Git.
func (c *Client) UpdateDCToGit(ucp UpdateComponentParams, isDeleteSupervisordVolumes bool) (err error) {

	// Find the container (don't want to use .Spec.Containers[0] in case the user has modified the DC...)
	existingCmpContainer, err := FindContainer(ucp.ExistingDC.Spec.Template.Spec.Containers, ucp.CommonObjectMeta.Name)
	if err != nil {
		return errors.Wrapf(err, "Unable to find container %s", ucp.CommonObjectMeta.Name)
	}

	// Fail if blank
	if ucp.ImageMeta.Name == "" {
		return errors.New("UpdateDCToGit imageName cannot be blank")
	}

	dc := generateGitDeploymentConfig(ucp.CommonObjectMeta, ucp.ImageMeta.Name, ucp.ImageMeta.Ports, ucp.EnvVars, &ucp.ResourceLimits)

	if isDeleteSupervisordVolumes {
		// Patch the current DC
		err = c.PatchCurrentDC(
			dc,
			removeTracesOfSupervisordFromDC,
			existingCmpContainer,
			ucp,
			true,
		)

		if err != nil {
			return errors.Wrapf(err, "unable to update the current DeploymentConfig %s", ucp.CommonObjectMeta.Name)
		}

		// Cleanup after the supervisor
		err = c.GetKubeClient().DeletePVC(getAppRootVolumeName(ucp.CommonObjectMeta.Name))
		if err != nil {
			return errors.Wrapf(err, "unable to delete S2I data PVC from %s", ucp.CommonObjectMeta.Name)
		}
	} else {
		err = c.PatchCurrentDC(
			dc,
			nil,
			existingCmpContainer,
			ucp,
			true,
		)
	}

	if err != nil {
		return errors.Wrapf(err, "unable to update the current DeploymentConfig %s", ucp.CommonObjectMeta.Name)
	}

	return nil
}

// UpdateDCToSupervisor updates the current DeploymentConfig to a SupervisorD configuration.
// Parameters:
//	commonObjectMeta: dc meta object
//	componentImageType: type of builder image
//	isToLocal: bool used to indicate if component is to be updated to local in which case a source backup dir will be injected into component env
//  isCreatePVC bool used to indicate if a new supervisorD PVC should be created during the update
// Returns:
//	errors if any or nil
func (c *Client) UpdateDCToSupervisor(ucp UpdateComponentParams, isToLocal bool, createPVC bool) error {

	existingCmpContainer, err := FindContainer(ucp.ExistingDC.Spec.Template.Spec.Containers, ucp.CommonObjectMeta.Name)
	if err != nil {
		return errors.Wrapf(err, "Unable to find container %s", ucp.CommonObjectMeta.Name)
	}

	// Retrieve the namespace of the corresponding component image
	imageStream, err := c.GetImageStream(ucp.ImageMeta.Namespace, ucp.ImageMeta.Name, ucp.ImageMeta.Tag)
	if err != nil {
		return errors.Wrap(err, "unable to get image stream for CreateBuildConfig")
	}
	ucp.ImageMeta.Namespace = imageStream.ObjectMeta.Namespace

	imageStreamImage, err := c.GetImageStreamImage(imageStream, ucp.ImageMeta.Tag)
	if err != nil {
		return errors.Wrap(err, "unable to bootstrap supervisord")
	}

	s2iPaths, err := GetS2IMetaInfoFromBuilderImg(imageStreamImage)
	if err != nil {
		return errors.Wrap(err, "unable to bootstrap supervisord")
	}

	cmpContainer := ucp.ExistingDC.Spec.Template.Spec.Containers[0]

	// Append s2i related parameters extracted above to env
	inputEnvs := injectS2IPaths(ucp.EnvVars, s2iPaths)

	if isToLocal {
		inputEnvs = uniqueAppendOrOverwriteEnvVars(
			inputEnvs,
			corev1.EnvVar{
				Name:  EnvS2ISrcBackupDir,
				Value: s2iPaths.SrcBackupPath,
			},
		)
	} else {
		inputEnvs = deleteEnvVars(inputEnvs, EnvS2ISrcBackupDir)
	}

	var dc appsv1.DeploymentConfig
	// if createPVC is true then we need to create a supervisorD volume and generate a new deployment config
	// needed for update from git to local/binary components
	// if false, we just update the current deployment config
	if createPVC {
		// Generate the SupervisorD Config
		dc = generateSupervisordDeploymentConfig(
			ucp.CommonObjectMeta,
			ucp.ImageMeta,
			inputEnvs,
			cmpContainer.EnvFrom,
			&ucp.ResourceLimits,
		)
		addInitVolumesToDC(&dc, ucp.CommonObjectMeta.Name, s2iPaths.DeploymentDir)

		ownerReference := GenerateOwnerReference(ucp.ExistingDC)

		// Setup PVC
		_, err = c.CreatePVC(getAppRootVolumeName(ucp.CommonObjectMeta.Name), "1Gi", ucp.CommonObjectMeta.Labels, ownerReference)
		if err != nil {
			return errors.Wrapf(err, "unable to create PVC for %s", ucp.CommonObjectMeta.Name)
		}
	} else {
		dc = updateSupervisorDeploymentConfig(
			SupervisorDUpdateParams{
				ucp.ExistingDC.DeepCopy(), ucp.CommonObjectMeta,
				ucp.ImageMeta,
				inputEnvs,
				cmpContainer.EnvFrom,
				&ucp.ResourceLimits,
			},
		)
	}

	// Patch the current DC with the new one
	err = c.PatchCurrentDC(
		dc,
		nil,
		existingCmpContainer,
		ucp,
		false,
	)
	if err != nil {
		return errors.Wrapf(err, "unable to update the current DeploymentConfig %s", ucp.CommonObjectMeta.Name)
	}

	return nil
}

func addInitVolumesToDC(dc *appsv1.DeploymentConfig, dcName string, deploymentDir string) {

	// Add the appropriate bootstrap volumes for SupervisorD
	addBootstrapVolumeCopyInitContainer(dc, dcName)
	addBootstrapSupervisordInitContainer(dc, dcName)
	addBootstrapVolume(dc, dcName)
	addBootstrapVolumeMount(dc, dcName)
	// only use the deployment Directory volume mount if its being used and
	// its not a sub directory of src_or_bin_path
	if deploymentDir != "" && !isSubDir(DefaultAppRootDir, deploymentDir) {
		addDeploymentDirVolumeMount(dc, deploymentDir)
	}
}

// UpdateDCAnnotations updates the DeploymentConfig file
// dcName is the name of the DeploymentConfig file to be updated
// annotations contains the annotations for the DeploymentConfig file
func (c *Client) UpdateDCAnnotations(dcName string, annotations map[string]string) error {
	dc, err := c.GetDeploymentConfigFromName(dcName)
	if err != nil {
		return errors.Wrapf(err, "unable to get DeploymentConfig %s", dcName)
	}

	dc.Annotations = annotations
	_, err = c.appsClient.DeploymentConfigs(c.Namespace).Update(dc)
	if err != nil {
		return errors.Wrapf(err, "unable to uDeploymentConfig config %s", dcName)
	}
	return nil
}

// removeTracesOfSupervisordFromDC takes a DeploymentConfig and removes any traces of the supervisord from it
// so it removes things like supervisord volumes, volumes mounts and init containers
func removeTracesOfSupervisordFromDC(dc *appsv1.DeploymentConfig) error {
	dcName := dc.Name

	err := removeVolumeFromDC(getAppRootVolumeName(dcName), dc)
	if err != nil {
		return err
	}

	err = removeVolumeMountsFromDC(getAppRootVolumeName(dcName), dc)
	if err != nil {
		return err
	}

	// remove the one bootstrapped init container
	for i, container := range dc.Spec.Template.Spec.InitContainers {
		if container.Name == "copy-files-to-volume" {
			dc.Spec.Template.Spec.InitContainers = append(dc.Spec.Template.Spec.InitContainers[:i], dc.Spec.Template.Spec.InitContainers[i+1:]...)
		}
	}

	return nil
}

// GetLatestBuildName gets the name of the latest build
// buildConfigName is the name of the buildConfig for which we are fetching the build name
// returns the name of the latest build or the error
func (c *Client) GetLatestBuildName(buildConfigName string) (string, error) {
	buildConfig, err := c.buildClient.BuildConfigs(c.Namespace).Get(buildConfigName, metav1.GetOptions{})
	if err != nil {
		return "", errors.Wrap(err, "unable to get the latest build name")
	}
	return fmt.Sprintf("%s-%d", buildConfigName, buildConfig.Status.LastVersion), nil
}

// StartBuild starts new build as it is, returns name of the build stat was started
func (c *Client) StartBuild(name string) (string, error) {
	klog.V(3).Infof("Build %s started.", name)
	buildRequest := buildv1.BuildRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
	result, err := c.buildClient.BuildConfigs(c.Namespace).Instantiate(name, &buildRequest)
	if err != nil {
		return "", errors.Wrapf(err, "unable to instantiate BuildConfig for %s", name)
	}
	klog.V(3).Infof("Build %s for BuildConfig %s triggered.", name, result.Name)

	return result.Name, nil
}

// WaitForBuildToFinish block and waits for build to finish. Returns error if build failed or was canceled.
func (c *Client) WaitForBuildToFinish(buildName string, stdout io.Writer, buildTimeout time.Duration) error {
	// following indicates if we have already setup the following logic
	following := false
	klog.V(3).Infof("Waiting for %s  build to finish", buildName)

	// start a watch on the build resources and look for the given build name
	w, err := c.buildClient.Builds(c.Namespace).Watch(metav1.ListOptions{
		FieldSelector: fields.Set{"metadata.name": buildName}.AsSelector().String(),
	})
	if err != nil {
		return errors.Wrapf(err, "unable to watch build")
	}
	defer w.Stop()
	timeout := time.After(buildTimeout)
	for {
		select {
		// when a event is received regarding the given buildName
		case val, ok := <-w.ResultChan():
			if !ok {
				break
			}
			// cast the object returned to a build object and check the phase of the build
			if e, ok := val.Object.(*buildv1.Build); ok {
				klog.V(3).Infof("Status of %s build is %s", e.Name, e.Status.Phase)
				switch e.Status.Phase {
				case buildv1.BuildPhaseComplete:
					// the build is completed thus return
					klog.V(3).Infof("Build %s completed.", e.Name)
					return nil
				case buildv1.BuildPhaseFailed, buildv1.BuildPhaseCancelled, buildv1.BuildPhaseError:
					// the build failed/got cancelled/error occurred thus return with error
					return errors.Errorf("build %s status %s", e.Name, e.Status.Phase)
				case buildv1.BuildPhaseRunning:
					// since the pod is ready and the build is now running, start following the logs
					if !following {
						// setting following to true as we need to set it up only once
						following = true
						err := c.FollowBuildLog(buildName, stdout, buildTimeout)
						if err != nil {
							return err
						}
					}
				}
			}
		case <-timeout:
			// timeout has occurred while waiting for the build to start/complete, so error out
			return errors.Errorf("timeout waiting for build %s to start", buildName)
		}
	}
}

// FollowBuildLog stream build log to stdout
func (c *Client) FollowBuildLog(buildName string, stdout io.Writer, buildTimeout time.Duration) error {
	buildLogOptions := buildv1.BuildLogOptions{
		Follow: true,
		NoWait: false,
	}

	rd, err := c.buildClient.RESTClient().Get().
		Timeout(buildTimeout).
		Namespace(c.Namespace).
		Resource("builds").
		Name(buildName).
		SubResource("log").
		VersionedParams(&buildLogOptions, buildschema.ParameterCodec).
		Stream()

	if err != nil {
		return errors.Wrapf(err, "unable get build log %s", buildName)
	}
	defer rd.Close()

	if _, err = io.Copy(stdout, rd); err != nil {
		return errors.Wrapf(err, "error streaming logs for %s", buildName)
	}

	return nil
}

// Delete takes labels as a input and based on it, deletes respective resource
func (c *Client) Delete(labels map[string]string, wait bool) error {

	// convert labels to selector
	selector := util.ConvertLabelsToSelector(labels)
	klog.V(3).Infof("Selectors used for deletion: %s", selector)

	var errorList []string
	var deletionPolicy = metav1.DeletePropagationBackground

	// for --wait flag, it deletes component dependents first and then delete component
	if wait {
		deletionPolicy = metav1.DeletePropagationForeground
	}
	// Delete DeploymentConfig
	klog.V(3).Info("Deleting DeploymentConfigs")
	err := c.appsClient.DeploymentConfigs(c.Namespace).DeleteCollection(&metav1.DeleteOptions{PropagationPolicy: &deletionPolicy}, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		errorList = append(errorList, "unable to delete deploymentconfig")
	}
	// Delete BuildConfig
	klog.V(3).Info("Deleting BuildConfigs")
	err = c.buildClient.BuildConfigs(c.Namespace).DeleteCollection(&metav1.DeleteOptions{}, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		errorList = append(errorList, "unable to delete buildconfig")
	}
	// Delete ImageStream
	klog.V(3).Info("Deleting ImageStreams")
	err = c.imageClient.ImageStreams(c.Namespace).DeleteCollection(&metav1.DeleteOptions{}, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		errorList = append(errorList, "unable to delete imagestream")
	}

	// for --wait it waits for component to be deleted
	// TODO: Need to modify for `odo app delete`, currently wait flag is added only in `odo component delete`
	//       so only one component gets passed in selector
	if wait {
		err = c.WaitForComponentDeletion(selector)
		if err != nil {
			errorList = append(errorList, err.Error())
		}
	}

	// Error string
	errString := strings.Join(errorList, ",")
	if len(errString) != 0 {
		return errors.New(errString)
	}
	return nil

}

// WaitForComponentDeletion waits for component to be deleted
func (c *Client) WaitForComponentDeletion(selector string) error {

	klog.V(3).Infof("Waiting for component to get deleted")

	watcher, err := c.appsClient.DeploymentConfigs(c.Namespace).Watch(metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return err
	}
	defer watcher.Stop()
	eventCh := watcher.ResultChan()

	for {
		select {
		case event, ok := <-eventCh:
			_, typeOk := event.Object.(*appsv1.DeploymentConfig)
			if !ok || !typeOk {
				return errors.New("Unable to watch deployment config")
			}
			if event.Type == watch.Deleted {
				klog.V(3).Infof("WaitForComponentDeletion, Event Received:Deleted")
				return nil
			} else if event.Type == watch.Error {
				klog.V(3).Infof("WaitForComponentDeletion, Event Received:Deleted ")
				return errors.New("Unable to watch deployment config")
			}
		case <-time.After(waitForComponentDeletionTimeout):
			klog.V(3).Infof("WaitForComponentDeletion, Timeout")
			return errors.New("Time out waiting for component to get deleted")
		}
	}
}

// DeleteServiceInstance takes labels as a input and based on it, deletes respective service instance
func (c *Client) DeleteServiceInstance(labels map[string]string) error {
	klog.V(3).Infof("Deleting Service Instance")

	// convert labels to selector
	selector := util.ConvertLabelsToSelector(labels)
	klog.V(3).Infof("Selectors used for deletion: %s", selector)

	// Listing out serviceInstance because `DeleteCollection` method don't work on serviceInstance
	serviceInstances, err := c.GetServiceInstanceList(selector)
	if err != nil {
		return errors.Wrap(err, "unable to list service instance")
	}

	// Iterating over serviceInstance List and deleting one by one
	for _, serviceInstance := range serviceInstances {
		// we need to delete the ServiceBinding before deleting the ServiceInstance
		err = c.serviceCatalogClient.ServiceBindings(c.Namespace).Delete(serviceInstance.Name, &metav1.DeleteOptions{})
		if err != nil {
			return errors.Wrap(err, "unable to delete serviceBinding")
		}
		// now we perform the actual deletion
		err = c.serviceCatalogClient.ServiceInstances(c.Namespace).Delete(serviceInstance.Name, &metav1.DeleteOptions{})
		if err != nil {
			return errors.Wrap(err, "unable to delete serviceInstance")
		}
	}

	return nil
}

// GetServiceInstanceLabelValues get label values of given label from objects in project that match the selector
func (c *Client) GetServiceInstanceLabelValues(label string, selector string) ([]string, error) {

	// List ServiceInstance according to given selectors
	svcList, err := c.serviceCatalogClient.ServiceInstances(c.Namespace).List(metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return nil, errors.Wrap(err, "unable to list ServiceInstances")
	}

	// Grab all the matched strings
	var values []string
	for _, elem := range svcList.Items {
		val, ok := elem.Labels[label]
		if ok {
			values = append(values, val)
		}
	}

	// Sort alphabetically
	sort.Strings(values)

	return values, nil
}

// GetServiceInstanceList returns list service instances
func (c *Client) GetServiceInstanceList(selector string) ([]scv1beta1.ServiceInstance, error) {
	// List ServiceInstance according to given selectors
	svcList, err := c.serviceCatalogClient.ServiceInstances(c.Namespace).List(metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return nil, errors.Wrap(err, "unable to list ServiceInstances")
	}

	return svcList.Items, nil
}

// GetBuildConfigFromName get BuildConfig by its name
func (c *Client) GetBuildConfigFromName(name string) (*buildv1.BuildConfig, error) {
	klog.V(3).Infof("Getting BuildConfig: %s", name)
	bc, err := c.buildClient.BuildConfigs(c.Namespace).Get(name, metav1.GetOptions{})
	if err != nil {
		return nil, errors.Wrapf(err, "unable to get BuildConfig %s", name)
	}
	return bc, nil
}

// GetClusterServiceClasses queries the service service catalog to get available clusterServiceClasses
func (c *Client) GetClusterServiceClasses() ([]scv1beta1.ClusterServiceClass, error) {
	classList, err := c.serviceCatalogClient.ClusterServiceClasses().List(metav1.ListOptions{})
	if err != nil {
		return nil, errors.Wrap(err, "unable to list cluster service classes")
	}
	return classList.Items, nil
}

// GetClusterServiceClass returns the required service class from the service name
// serviceName is the name of the service
// returns the required service class and the error
func (c *Client) GetClusterServiceClass(serviceName string) (*scv1beta1.ClusterServiceClass, error) {
	opts := metav1.ListOptions{
		FieldSelector: fields.OneTermEqualSelector("spec.externalName", serviceName).String(),
	}
	searchResults, err := c.serviceCatalogClient.ClusterServiceClasses().List(opts)
	if err != nil {
		return nil, fmt.Errorf("unable to search classes by name (%s)", err)
	}
	if len(searchResults.Items) == 0 {
		return nil, fmt.Errorf("class '%s' not found", serviceName)
	}
	if len(searchResults.Items) > 1 {
		return nil, fmt.Errorf("more than one matching class found for '%s'", serviceName)
	}
	return &searchResults.Items[0], nil
}

// GetClusterPlansFromServiceName returns the plans associated with a service class
// serviceName is the name (the actual id, NOT the external name) of the service class whose plans are required
// returns array of ClusterServicePlans or error
func (c *Client) GetClusterPlansFromServiceName(serviceName string) ([]scv1beta1.ClusterServicePlan, error) {
	opts := metav1.ListOptions{
		FieldSelector: fields.OneTermEqualSelector("spec.clusterServiceClassRef.name", serviceName).String(),
	}
	searchResults, err := c.serviceCatalogClient.ClusterServicePlans().List(opts)
	if err != nil {
		return nil, fmt.Errorf("unable to search plans for service name '%s', (%s)", serviceName, err)
	}
	return searchResults.Items, nil
}

// CreateServiceInstance creates service instance from service catalog
func (c *Client) CreateServiceInstance(serviceName string, serviceType string, servicePlan string, parameters map[string]string, labels map[string]string) error {
	serviceInstanceParameters, err := serviceInstanceParameters(parameters)
	if err != nil {
		return errors.Wrap(err, "unable to create the service instance parameters")
	}

	_, err = c.serviceCatalogClient.ServiceInstances(c.Namespace).Create(
		&scv1beta1.ServiceInstance{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ServiceInstance",
				APIVersion: "servicecatalog.k8s.io/v1beta1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      serviceName,
				Namespace: c.Namespace,
				Labels:    labels,
			},
			Spec: scv1beta1.ServiceInstanceSpec{
				PlanReference: scv1beta1.PlanReference{
					ClusterServiceClassExternalName: serviceType,
					ClusterServicePlanExternalName:  servicePlan,
				},
				Parameters: serviceInstanceParameters,
			},
		})

	if err != nil {
		return errors.Wrapf(err, "unable to create the service instance %s for the service type %s and plan %s", serviceName, serviceType, servicePlan)
	}

	// Create the secret containing the parameters of the plan selected.
	err = c.CreateServiceBinding(serviceName, c.Namespace, labels)
	if err != nil {
		return errors.Wrapf(err, "unable to create the secret %s for the service instance", serviceName)
	}

	return nil
}

// CreateServiceBinding creates a ServiceBinding (essentially a secret) within the namespace of the
// service instance created using the service's parameters.
func (c *Client) CreateServiceBinding(bindingName string, namespace string, labels map[string]string) error {
	_, err := c.serviceCatalogClient.ServiceBindings(namespace).Create(
		&scv1beta1.ServiceBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      bindingName,
				Namespace: namespace,
				Labels:    labels,
			},
			Spec: scv1beta1.ServiceBindingSpec{
				InstanceRef: scv1beta1.LocalObjectReference{
					Name: bindingName,
				},
				SecretName: bindingName,
			},
		})

	if err != nil {
		return errors.Wrap(err, "Creation of the secret failed")
	}

	return nil
}

// GetServiceBinding returns the ServiceBinding named serviceName in the namespace namespace
func (c *Client) GetServiceBinding(serviceName string, namespace string) (*scv1beta1.ServiceBinding, error) {
	return c.serviceCatalogClient.ServiceBindings(namespace).Get(serviceName, metav1.GetOptions{})
}

// serviceInstanceParameters converts a map of variable assignments to a byte encoded json document,
// which is what the ServiceCatalog API consumes.
func serviceInstanceParameters(params map[string]string) (*runtime.RawExtension, error) {
	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	return &runtime.RawExtension{Raw: paramsJSON}, nil
}

// Define a function that is meant to create patch based on the contents of the DC
type dcPatchProvider func(dc *appsv1.DeploymentConfig) (string, error)

// LinkSecret links a secret to the DeploymentConfig of a component
func (c *Client) LinkSecret(secretName, componentName, applicationName string) error {

	var dcPatchProvider = func(dc *appsv1.DeploymentConfig) (string, error) {
		if len(dc.Spec.Template.Spec.Containers[0].EnvFrom) > 0 {
			// we always add the link as the first value in the envFrom array. That way we don't need to know the existing value
			return fmt.Sprintf(`[{ "op": "add", "path": "/spec/template/spec/containers/0/envFrom/0", "value": {"secretRef": {"name": "%s"}} }]`, secretName), nil
		}

		//in this case we need to add the full envFrom value
		return fmt.Sprintf(`[{ "op": "add", "path": "/spec/template/spec/containers/0/envFrom", "value": [{"secretRef": {"name": "%s"}}] }]`, secretName), nil
	}

	return c.patchDCOfComponent(componentName, applicationName, dcPatchProvider)
}

// UnlinkSecret unlinks a secret to the DeploymentConfig of a component
func (c *Client) UnlinkSecret(secretName, componentName, applicationName string) error {
	// Remove the Secret from the container
	var dcPatchProvider = func(dc *appsv1.DeploymentConfig) (string, error) {
		indexForRemoval := -1
		for i, env := range dc.Spec.Template.Spec.Containers[0].EnvFrom {
			if env.SecretRef.Name == secretName {
				indexForRemoval = i
				break
			}
		}

		if indexForRemoval == -1 {
			return "", fmt.Errorf("DeploymentConfig does not contain a link to %s", secretName)
		}

		return fmt.Sprintf(`[{"op": "remove", "path": "/spec/template/spec/containers/0/envFrom/%d"}]`, indexForRemoval), nil
	}

	return c.patchDCOfComponent(componentName, applicationName, dcPatchProvider)
}

// this function will look up the appropriate DC, and execute the specified patch
// the whole point of using patch is to avoid race conditions where we try to update
// dc while it's being simultaneously updated from another source (for example Kubernetes itself)
// this will result in the triggering of a redeployment
func (c *Client) patchDCOfComponent(componentName, applicationName string, dcPatchProvider dcPatchProvider) error {
	dcName, err := util.NamespaceOpenShiftObject(componentName, applicationName)
	if err != nil {
		return err
	}

	dc, err := c.appsClient.DeploymentConfigs(c.Namespace).Get(dcName, metav1.GetOptions{})
	if err != nil {
		return errors.Wrapf(err, "Unable to locate DeploymentConfig for component %s of application %s", componentName, applicationName)
	}

	if dcPatchProvider != nil {
		patch, err := dcPatchProvider(dc)
		if err != nil {
			return errors.Wrap(err, "Unable to create a patch for the DeploymentConfig")
		}

		// patch the DeploymentConfig with the secret
		_, err = c.appsClient.DeploymentConfigs(c.Namespace).Patch(dcName, types.JSONPatchType, []byte(patch))
		if err != nil {
			return errors.Wrapf(err, "DeploymentConfig not patched %s", dc.Name)
		}
	} else {
		return errors.Wrapf(err, "dcPatch was not properly set")
	}

	return nil
}

// GetServiceClassesByCategory retrieves a map associating category name to ClusterServiceClasses matching the category
func (c *Client) GetServiceClassesByCategory() (categories map[string][]scv1beta1.ClusterServiceClass, err error) {
	categories = make(map[string][]scv1beta1.ClusterServiceClass)
	classes, err := c.GetClusterServiceClasses()
	if err != nil {
		return nil, err
	}

	// TODO: Should we replicate the classification performed in
	// https://github.com/openshift/console/blob/master/frontend/public/components/catalog/catalog-items.jsx?
	for _, class := range classes {
		tags := class.Spec.Tags
		category := "other"
		if len(tags) > 0 && len(tags[0]) > 0 {
			category = tags[0]
		}
		categories[category] = append(categories[category], class)
	}

	return categories, err
}

// GetMatchingPlans retrieves a map associating service plan name to service plan instance associated with the specified service
// class
func (c *Client) GetMatchingPlans(class scv1beta1.ClusterServiceClass) (plans map[string]scv1beta1.ClusterServicePlan, err error) {
	planList, err := c.serviceCatalogClient.ClusterServicePlans().List(metav1.ListOptions{
		FieldSelector: "spec.clusterServiceClassRef.name==" + class.Spec.ExternalID,
	})

	plans = make(map[string]scv1beta1.ClusterServicePlan)
	for _, v := range planList.Items {
		plans[v.Spec.ExternalName] = v
	}
	return plans, err
}

// GetAllClusterServicePlans returns list of available plans
func (c *Client) GetAllClusterServicePlans() ([]scv1beta1.ClusterServicePlan, error) {
	planList, err := c.serviceCatalogClient.ClusterServicePlans().List(metav1.ListOptions{})
	if err != nil {
		return nil, errors.Wrap(err, "unable to get cluster service plan")
	}

	return planList.Items, nil
}

// DeleteBuildConfig deletes the given BuildConfig by name using CommonObjectMeta..
func (c *Client) DeleteBuildConfig(commonObjectMeta metav1.ObjectMeta) error {

	// Convert labels to selector
	selector := util.ConvertLabelsToSelector(commonObjectMeta.Labels)
	klog.V(3).Infof("DeleteBuildConfig selectors used for deletion: %s", selector)

	// Delete BuildConfig
	klog.V(3).Info("Deleting BuildConfigs with DeleteBuildConfig")
	return c.buildClient.BuildConfigs(c.Namespace).DeleteCollection(&metav1.DeleteOptions{}, metav1.ListOptions{LabelSelector: selector})
}

// AddEnvironmentVariablesToDeploymentConfig adds the given environment
// variables to the only container in the Deployment Config and updates in the
// cluster
func (c *Client) AddEnvironmentVariablesToDeploymentConfig(envs []corev1.EnvVar, dc *appsv1.DeploymentConfig) error {
	numContainers := len(dc.Spec.Template.Spec.Containers)
	if numContainers != 1 {
		return fmt.Errorf("expected exactly one container in Deployment Config %v, got %v", dc.Name, numContainers)
	}

	dc.Spec.Template.Spec.Containers[0].Env = append(dc.Spec.Template.Spec.Containers[0].Env, envs...)

	_, err := c.appsClient.DeploymentConfigs(c.Namespace).Update(dc)
	if err != nil {
		return errors.Wrapf(err, "unable to update Deployment Config %v", dc.Name)
	}
	return nil
}

// ServerInfo contains the fields that contain the server's information like
// address, OpenShift and Kubernetes versions
type ServerInfo struct {
	Address           string
	OpenShiftVersion  string
	KubernetesVersion string
}

// GetServerVersion will fetch the Server Host, OpenShift and Kubernetes Version
// It will be shown on the execution of odo version command
func (c *Client) GetServerVersion() (*ServerInfo, error) {
	var info ServerInfo

	// This will fetch the information about Server Address
	config, err := c.KubeConfig.ClientConfig()
	if err != nil {
		return nil, errors.Wrapf(err, "unable to get server's address")
	}
	info.Address = config.Host

	// checking if the server is reachable
	if !isServerUp(config.Host) {
		return nil, errors.New("Unable to connect to OpenShift cluster, is it down?")
	}

	// fail fast if user is not connected (same logic as `oc whoami`)
	_, err = c.userClient.Users().Get("~", metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	// This will fetch the information about OpenShift Version
	coreGet := c.kubeClient.KubeClient.CoreV1().RESTClient().Get()
	rawOpenShiftVersion, err := coreGet.AbsPath("/version/openshift").Do().Raw()
	if err != nil {
		// when using Minishift (or plain 'oc cluster up' for that matter) with OKD 3.11, the version endpoint is missing...
		klog.V(3).Infof("Unable to get OpenShift Version - endpoint '/version/openshift' doesn't exist")
	} else {
		var openShiftVersion version.Info
		if err := json.Unmarshal(rawOpenShiftVersion, &openShiftVersion); err != nil {
			return nil, errors.Wrapf(err, "unable to unmarshal OpenShift version %v", string(rawOpenShiftVersion))
		}
		info.OpenShiftVersion = openShiftVersion.GitVersion
	}

	// This will fetch the information about Kubernetes Version
	rawKubernetesVersion, err := coreGet.AbsPath("/version").Do().Raw()
	if err != nil {
		return nil, errors.Wrapf(err, "unable to get Kubernetes Version")
	}
	var kubernetesVersion version.Info
	if err := json.Unmarshal(rawKubernetesVersion, &kubernetesVersion); err != nil {
		return nil, errors.Wrapf(err, "unable to unmarshal Kubernetes Version: %v", string(rawKubernetesVersion))
	}
	info.KubernetesVersion = kubernetesVersion.GitVersion

	return &info, nil
}

// ExecCMDInContainer execute command in the specified container of a pod. If `containerName` is blank, it execs in the first container.
func (c *Client) ExecCMDInContainer(compInfo common.ComponentInfo, cmd []string, stdout io.Writer, stderr io.Writer, stdin io.Reader, tty bool) error {
	podExecOptions := corev1.PodExecOptions{
		Command: cmd,
		Stdin:   stdin != nil,
		Stdout:  stdout != nil,
		Stderr:  stderr != nil,
		TTY:     tty,
	}

	// If a container name was passed in, set it in the exec options, otherwise leave it blank
	if compInfo.ContainerName != "" {
		podExecOptions.Container = compInfo.ContainerName
	}

	req := c.kubeClient.KubeClient.CoreV1().RESTClient().
		Post().
		Namespace(c.Namespace).
		Resource("pods").
		Name(compInfo.PodName).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Command: cmd,
			Stdin:   stdin != nil,
			Stdout:  stdout != nil,
			Stderr:  stderr != nil,
			TTY:     tty,
		}, scheme.ParameterCodec)

	config, err := c.KubeConfig.ClientConfig()
	if err != nil {
		return errors.Wrapf(err, "unable to get Kubernetes client config")
	}

	// Connect to url (constructed from req) using SPDY (HTTP/2) protocol which allows bidirectional streams.
	exec, err := remotecommand.NewSPDYExecutor(config, "POST", req.URL())
	if err != nil {
		return errors.Wrapf(err, "unable execute command via SPDY")
	}
	// initialize the transport of the standard shell streams
	err = exec.Stream(remotecommand.StreamOptions{
		Stdin:  stdin,
		Stdout: stdout,
		Stderr: stderr,
		Tty:    tty,
	})
	if err != nil {
		return errors.Wrapf(err, "error while streaming command")
	}

	return nil
}

// ExtractProjectToComponent extracts the project archive(tar) to the target path from the reader stdin
func (c *Client) ExtractProjectToComponent(compInfo common.ComponentInfo, targetPath string, stdin io.Reader) error {
	// cmdArr will run inside container
	cmdArr := []string{"tar", "xf", "-", "-C", targetPath}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	klog.V(3).Infof("Executing command %s", strings.Join(cmdArr, " "))
	err := c.ExecCMDInContainer(compInfo, cmdArr, &stdout, &stderr, stdin, false)
	if err != nil {
		log.Errorf("Command '%s' in container failed.\n", strings.Join(cmdArr, " "))
		log.Errorf("stdout: %s\n", stdout.String())
		log.Errorf("stderr: %s\n", stderr.String())
		log.Errorf("err: %s\n", err.Error())
		return err
	}
	return nil
}

func (c *Client) GetKubeClient() *kclient.Client {
	return c.kubeClient
}

func (c *Client) SetKubeClient(client *kclient.Client) {
	c.kubeClient = client
}

// GetVolumeMountsFromDC returns a list of all volume mounts in the given DC
func (c *Client) GetVolumeMountsFromDC(dc *appsv1.DeploymentConfig) []corev1.VolumeMount {
	var volumeMounts []corev1.VolumeMount
	for _, container := range dc.Spec.Template.Spec.Containers {
		volumeMounts = append(volumeMounts, container.VolumeMounts...)
	}
	return volumeMounts
}

// IsVolumeAnEmptyDir returns true if the volume is an EmptyDir, false if not
func (c *Client) IsVolumeAnEmptyDir(volumeMountName string, dc *appsv1.DeploymentConfig) bool {
	for _, volume := range dc.Spec.Template.Spec.Volumes {
		if volume.Name == volumeMountName {
			if volume.EmptyDir != nil {
				return true
			}
		}
	}
	return false
}

// IsVolumeAnConfigMap returns true if the volume is an ConfigMap, false if not
func (c *Client) IsVolumeAnConfigMap(volumeMountName string, dc *appsv1.DeploymentConfig) bool {
	for _, volume := range dc.Spec.Template.Spec.Volumes {
		if volume.Name == volumeMountName {
			if volume.ConfigMap != nil {
				return true
			}
		}
	}
	return false
}

// CreateBuildConfig creates a buildConfig using the builderImage as well as gitURL.
// envVars is the array containing the environment variables
func (c *Client) CreateBuildConfig(commonObjectMeta metav1.ObjectMeta, builderImage string, gitURL string, gitRef string, envVars []corev1.EnvVar) (buildv1.BuildConfig, error) {

	// Retrieve the namespace, image name and the appropriate tag
	imageNS, imageName, imageTag, _, err := ParseImageName(builderImage)
	if err != nil {
		return buildv1.BuildConfig{}, errors.Wrap(err, "unable to parse image name")
	}
	imageStream, err := c.GetImageStream(imageNS, imageName, imageTag)
	if err != nil {
		return buildv1.BuildConfig{}, errors.Wrap(err, "unable to retrieve image stream for CreateBuildConfig")
	}
	imageNS = imageStream.ObjectMeta.Namespace

	klog.V(3).Infof("Using namespace: %s for the CreateBuildConfig function", imageNS)

	// Use BuildConfig to build the container with Git
	bc := generateBuildConfig(commonObjectMeta, gitURL, gitRef, imageName+":"+imageTag, imageNS)

	if len(envVars) > 0 {
		bc.Spec.Strategy.SourceStrategy.Env = envVars
	}
	_, err = c.buildClient.BuildConfigs(c.Namespace).Create(&bc)
	if err != nil {
		return buildv1.BuildConfig{}, errors.Wrapf(err, "unable to create BuildConfig for %s", commonObjectMeta.Name)
	}

	return bc, nil
}

// FindContainer finds the container
func FindContainer(containers []corev1.Container, name string) (corev1.Container, error) {

	if name == "" {
		return corev1.Container{}, errors.New("Invalid parameter for FindContainer, unable to find a blank container")
	}

	for _, container := range containers {
		if container.Name == name {
			return container, nil
		}
	}

	return corev1.Container{}, errors.New("Unable to find container")
}

// GetInputEnvVarsFromStrings generates corev1.EnvVar values from the array of string key=value pairs
// envVars is the array containing the key=value pairs
func GetInputEnvVarsFromStrings(envVars []string) ([]corev1.EnvVar, error) {
	var inputEnvVars []corev1.EnvVar
	var keys = make(map[string]int)
	for _, env := range envVars {
		splits := strings.SplitN(env, "=", 2)
		if len(splits) < 2 {
			return nil, errors.New("invalid syntax for env, please specify a VariableName=Value pair")
		}
		_, ok := keys[splits[0]]
		if ok {
			return nil, errors.Errorf("multiple values found for VariableName: %s", splits[0])
		}

		keys[splits[0]] = 1

		inputEnvVars = append(inputEnvVars, corev1.EnvVar{
			Name:  splits[0],
			Value: splits[1],
		})
	}
	return inputEnvVars, nil
}

// GetEnvVarsFromDC retrieves the env vars from the DC
// dcName is the name of the dc from which the env vars are retrieved
// projectName is the name of the project
func (c *Client) GetEnvVarsFromDC(dcName string) ([]corev1.EnvVar, error) {
	dc, err := c.GetDeploymentConfigFromName(dcName)
	if err != nil {
		return nil, errors.Wrap(err, "error occurred while retrieving the dc")
	}

	numContainers := len(dc.Spec.Template.Spec.Containers)
	if numContainers != 1 {
		return nil, fmt.Errorf("expected exactly one container in Deployment Config %v, got %v", dc.Name, numContainers)
	}

	return dc.Spec.Template.Spec.Containers[0].Env, nil
}

// PropagateDeletes deletes the watch detected deleted files from remote component pod from each of the paths in passed s2iPaths
// Parameters:
//	targetPodName: Name of component pod
//	delSrcRelPaths: Paths to be deleted on the remote pod relative to component source base path ex: Component src: /abc/src, file deleted: abc/src/foo.lang => relative path: foo.lang
//	s2iPaths: Slice of all s2i paths -- deployment dir, destination dir, working dir, etc..
func (c *Client) PropagateDeletes(targetPodName string, delSrcRelPaths []string, s2iPaths []string) error {
	reader, writer := io.Pipe()
	var rmPaths []string
	if len(s2iPaths) == 0 || len(delSrcRelPaths) == 0 {
		return fmt.Errorf("Failed to propagate deletions: s2iPaths: %+v and delSrcRelPaths: %+v", s2iPaths, delSrcRelPaths)
	}
	for _, s2iPath := range s2iPaths {
		for _, delRelPath := range delSrcRelPaths {
			// since the paths inside the container are linux oriented
			// so we convert the paths accordingly
			rmPaths = append(rmPaths, filepath.ToSlash(filepath.Join(s2iPath, delRelPath)))
		}
	}
	klog.V(3).Infof("s2ipaths marked for deletion are %+v", rmPaths)
	cmdArr := []string{"rm", "-rf"}
	cmdArr = append(cmdArr, rmPaths...)
	compInfo := common.ComponentInfo{
		PodName: targetPodName,
	}

	err := c.ExecCMDInContainer(compInfo, cmdArr, writer, writer, reader, false)
	if err != nil {
		return err
	}
	return err
}

func injectS2IPaths(existingVars []corev1.EnvVar, s2iPaths S2IPaths) []corev1.EnvVar {
	return uniqueAppendOrOverwriteEnvVars(
		existingVars,
		corev1.EnvVar{
			Name:  EnvS2IScriptsURL,
			Value: s2iPaths.ScriptsPath,
		},
		corev1.EnvVar{
			Name:  EnvS2IScriptsProtocol,
			Value: s2iPaths.ScriptsPathProtocol,
		},
		corev1.EnvVar{
			Name:  EnvS2ISrcOrBinPath,
			Value: s2iPaths.SrcOrBinPath,
		},
		corev1.EnvVar{
			Name:  EnvS2IDeploymentDir,
			Value: s2iPaths.DeploymentDir,
		},
		corev1.EnvVar{
			Name:  EnvS2IWorkingDir,
			Value: s2iPaths.WorkingDir,
		},
		corev1.EnvVar{
			Name:  EnvS2IBuilderImageName,
			Value: s2iPaths.BuilderImgName,
		},
	)

}

func isSubDir(baseDir, otherDir string) bool {
	cleanedBaseDir := filepath.Clean(baseDir)
	cleanedOtherDir := filepath.Clean(otherDir)
	if cleanedBaseDir == cleanedOtherDir {
		return true
	}
	//matches, _ := filepath.Match(fmt.Sprintf("%s/*", cleanedBaseDir), cleanedOtherDir)
	matches, _ := filepath.Match(filepath.Join(cleanedBaseDir, "*"), cleanedOtherDir)
	return matches
}

// IsImageStreamSupported checks if imagestream resource type is present on the cluster
func (c *Client) IsImageStreamSupported() (bool, error) {

	return c.isResourceSupported("image.openshift.io", "v1", "imagestreams")
}

// IsSBRSupported checks if resource of type service binding request present on the cluster
func (c *Client) IsSBRSupported() (bool, error) {
	return c.isResourceSupported("apps.openshift.io", "v1alpha1", "servicebindingrequests")
}

// IsCSVSupported checks if resource of type service binding request present on the cluster
func (c *Client) IsCSVSupported() (bool, error) {
	return c.isResourceSupported("operators.coreos.com", "v1alpha1", "clusterserviceversions")
}

// GenerateOwnerReference generates an ownerReference which can then be set as
// owner for various OpenShift objects and ensure that when the owner object is
// deleted from the cluster, all other objects are automatically removed by
// OpenShift garbage collector
func GenerateOwnerReference(dc *appsv1.DeploymentConfig) metav1.OwnerReference {

	ownerReference := metav1.OwnerReference{
		APIVersion: "apps.openshift.io/v1",
		Kind:       "DeploymentConfig",
		Name:       dc.Name,
		UID:        dc.UID,
	}

	return ownerReference
}

func (c *Client) isResourceSupported(apiGroup, apiVersion, resourceName string) (bool, error) {
	if c.supportedResources == nil {
		c.supportedResources = make(map[string]bool, 7)
	}
	groupVersion := metav1.GroupVersion{Group: apiGroup, Version: apiVersion}.String()

	supported, found := c.supportedResources[groupVersion]
	if !found {
		list, err := c.discoveryClient.ServerResourcesForGroupVersion(groupVersion)
		if err != nil {
			if kerrors.IsNotFound(err) {
				supported = false
			} else {
				// don't record, just attempt again next time in case it's a transient error
				return false, err
			}
		} else {
			for _, resources := range list.APIResources {
				if resources.Name == resourceName {
					supported = true
					break
				}
			}
		}
		c.supportedResources[groupVersion] = supported
	}
	return supported, nil
}
