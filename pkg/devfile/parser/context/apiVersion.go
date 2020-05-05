package parser

import (
	"encoding/json"
	"fmt"

	"github.com/golang/glog"
	"github.com/openshift/odo/pkg/devfile/parser/data"
	"github.com/pkg/errors"
)

// SetDevfileAPIVersion returns the devfile APIVersion
func (d *DevfileCtx) SetDevfileAPIVersion() error {

	// Unmarshal JSON into map
	var r map[string]interface{}
	err := json.Unmarshal(d.rawContent, &r)
	if err != nil {
		return errors.Wrapf(err, "failed to decode devfile json")
	}

	var schemaVersion interface{}
	var okschema bool
	// Get "apiVersion" value from the map
	apiVersion, okapi := r["apiVersion"]
	if !okapi {
		// for devfile 2.0
		schemaVersion, okschema = r["schemaVersion"]
		if !okschema {
			return fmt.Errorf("apiVersion or schemaVersion not present in devfile")
		}
	}

	/*
		// apiVersion cannot be empty
		if apiVersion.(string) == "" && schemaVersion.(string) == "" {

			return fmt.Errorf("apiVersion or schemaVersion in devfile cannot be empty")
		}*/

	if okapi {
		if apiVersion.(string) != "" {
			d.apiVersion = apiVersion.(string)
			glog.V(4).Infof("devfile apiVersion: '%s'", d.apiVersion)

		}

	}

	if okschema {

		if schemaVersion.(string) != "" {
			d.apiVersion = schemaVersion.(string)
			glog.V(4).Infof("devfile schemaVersion: '%s'", d.apiVersion)

		}
	}

	return nil
}

// GetApiVersion returns apiVersion stored in devfile context
func (d *DevfileCtx) GetApiVersion() string {
	return d.apiVersion
}

// IsApiVersionSupported return true if the apiVersion in DevfileCtx is supported in odo
func (d *DevfileCtx) IsApiVersionSupported() bool {
	return data.IsApiVersionSupported(d.apiVersion)
}
