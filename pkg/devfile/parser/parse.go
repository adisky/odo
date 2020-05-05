package parser

import (
	"encoding/json"
	"fmt"

	devfileCtx "github.com/openshift/odo/pkg/devfile/parser/context"

	"github.com/openshift/odo/pkg/devfile/parser/data"
	"github.com/pkg/errors"
)

const (
	apiVersion100 = "1.0.0"
	apiVersion200 = "2.0.0"
)

// Parse func parses and validates the devfile integrity.
// Creates devfile context and runtime objects
func Parse(path string) (d DevfileObj, err error) {

	// NewDevfileCtx
	d.Ctx = devfileCtx.NewDevfileCtx(path)

	// Fill the fields of DevfileCtx struct
	err = d.Ctx.Populate()
	if err != nil {
		return d, err
	}

	// Validate devfile
	err = d.Ctx.Validate()
	if err != nil {
		return d, err
	}

	/*// Create a new devfile data object
	d.Data, err = data.NewDevfileData(d.Ctx.GetApiVersion())
	if err != nil {
		return d, err
	}*/

	if d.Ctx.GetApiVersion() == apiVersion100 {
		fmt.Println("@adi apiversion100")
		d.Data = data.V100{}

		a := d.Data.(data.V100)
		err = json.Unmarshal(d.Ctx.GetDevfileContent(), &a.Devfile)
		if err != nil {
			return d, errors.Wrapf(err, "failed to decode devfile content")
		}

		d.Data = data.V100{Devfile: a.Devfile}

	}

	if d.Ctx.GetApiVersion() == apiVersion200 {
		fmt.Println("@adi apiversion200")
		d.Data = data.V200{}
		a := d.Data.(data.V200)
		err = json.Unmarshal(d.Ctx.GetDevfileContent(), &a.Devfile)
		if err != nil {
			return d, errors.Wrapf(err, "failed to decode devfile content")
		}

		d.Data = data.V200{Devfile: a.Devfile}

	}

	// Unmarshal devfile content into devfile struct

	// Successful
	return d, nil
}
