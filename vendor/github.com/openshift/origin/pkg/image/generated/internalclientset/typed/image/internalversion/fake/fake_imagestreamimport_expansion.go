// Code generated by client-gen. DO NOT EDIT.

package fake

import (
	image "github.com/openshift/origin/pkg/image/apis/image"
	testing "k8s.io/client-go/testing"
)

func (c *FakeImageStreamImports) CreateWithoutTimeout(imageStreamImport *image.ImageStreamImport) (result *image.ImageStreamImport, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewCreateAction(imagestreamimportsResource, c.ns, imageStreamImport), &image.ImageStreamImport{})

	if obj == nil {
		return nil, err
	}
	return obj.(*image.ImageStreamImport), err
}