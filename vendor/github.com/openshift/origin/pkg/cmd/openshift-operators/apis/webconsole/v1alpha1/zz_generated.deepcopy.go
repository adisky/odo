// +build !ignore_autogenerated

// Code generated by deepcopy-gen. DO NOT EDIT.

package v1alpha1

import (
	runtime "k8s.io/apimachinery/pkg/runtime"
)

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *OpenShiftWebConsoleConfig) DeepCopyInto(out *OpenShiftWebConsoleConfig) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new OpenShiftWebConsoleConfig.
func (in *OpenShiftWebConsoleConfig) DeepCopy() *OpenShiftWebConsoleConfig {
	if in == nil {
		return nil
	}
	out := new(OpenShiftWebConsoleConfig)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *OpenShiftWebConsoleConfig) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *OpenShiftWebConsoleConfigList) DeepCopyInto(out *OpenShiftWebConsoleConfigList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	out.ListMeta = in.ListMeta
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]OpenShiftWebConsoleConfig, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new OpenShiftWebConsoleConfigList.
func (in *OpenShiftWebConsoleConfigList) DeepCopy() *OpenShiftWebConsoleConfigList {
	if in == nil {
		return nil
	}
	out := new(OpenShiftWebConsoleConfigList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *OpenShiftWebConsoleConfigList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *OpenShiftWebConsoleConfigSpec) DeepCopyInto(out *OpenShiftWebConsoleConfigSpec) {
	*out = *in
	out.OperatorSpec = in.OperatorSpec
	in.WebConsoleConfig.DeepCopyInto(&out.WebConsoleConfig)
	if in.NodeSelector != nil {
		in, out := &in.NodeSelector, &out.NodeSelector
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new OpenShiftWebConsoleConfigSpec.
func (in *OpenShiftWebConsoleConfigSpec) DeepCopy() *OpenShiftWebConsoleConfigSpec {
	if in == nil {
		return nil
	}
	out := new(OpenShiftWebConsoleConfigSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *OpenShiftWebConsoleConfigStatus) DeepCopyInto(out *OpenShiftWebConsoleConfigStatus) {
	*out = *in
	in.OperatorStatus.DeepCopyInto(&out.OperatorStatus)
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new OpenShiftWebConsoleConfigStatus.
func (in *OpenShiftWebConsoleConfigStatus) DeepCopy() *OpenShiftWebConsoleConfigStatus {
	if in == nil {
		return nil
	}
	out := new(OpenShiftWebConsoleConfigStatus)
	in.DeepCopyInto(out)
	return out
}