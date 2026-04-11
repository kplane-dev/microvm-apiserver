package v1alpha1

import "k8s.io/apimachinery/pkg/runtime"

func (in *MicroVM) DeepCopyInto(out *MicroVM) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	out.Spec = in.Spec
	in.Status.DeepCopyInto(&out.Status)
}

func (in *MicroVM) DeepCopy() *MicroVM {
	if in == nil {
		return nil
	}
	out := new(MicroVM)
	in.DeepCopyInto(out)
	return out
}

func (in *MicroVM) DeepCopyObject() runtime.Object {
	return in.DeepCopy()
}

func (in *MicroVMStatus) DeepCopyInto(out *MicroVMStatus) {
	*out = *in
	if in.SSH != nil {
		out.SSH = new(SSHStatus)
		*out.SSH = *in.SSH
	}
}

func (in *MicroVMList) DeepCopyInto(out *MicroVMList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		out.Items = make([]MicroVM, len(in.Items))
		for i := range in.Items {
			in.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}

func (in *MicroVMList) DeepCopy() *MicroVMList {
	if in == nil {
		return nil
	}
	out := new(MicroVMList)
	in.DeepCopyInto(out)
	return out
}

func (in *MicroVMList) DeepCopyObject() runtime.Object {
	return in.DeepCopy()
}

func (in *MicroVMExecOptions) DeepCopyInto(out *MicroVMExecOptions) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	if in.Command != nil {
		out.Command = make([]string, len(in.Command))
		copy(out.Command, in.Command)
	}
}

func (in *MicroVMExecOptions) DeepCopy() *MicroVMExecOptions {
	if in == nil {
		return nil
	}
	out := new(MicroVMExecOptions)
	in.DeepCopyInto(out)
	return out
}

func (in *MicroVMExecOptions) DeepCopyObject() runtime.Object {
	return in.DeepCopy()
}

func (in *MicroVMBranchOptions) DeepCopyInto(out *MicroVMBranchOptions) {
	*out = *in
	out.TypeMeta = in.TypeMeta
}

func (in *MicroVMBranchOptions) DeepCopy() *MicroVMBranchOptions {
	if in == nil {
		return nil
	}
	out := new(MicroVMBranchOptions)
	in.DeepCopyInto(out)
	return out
}

func (in *MicroVMBranchOptions) DeepCopyObject() runtime.Object {
	return in.DeepCopy()
}
