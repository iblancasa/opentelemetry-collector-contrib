// Code generated by mdatagen. DO NOT EDIT.

package metadata

import (
	"go.opentelemetry.io/collector/pdata/pcommon"
)

// ResourceBuilder is a helper struct to build resources predefined in metadata.yaml.
// The ResourceBuilder is not thread-safe and must not to be used in multiple goroutines.
type ResourceBuilder struct {
	config ResourceAttributesConfig
	res    pcommon.Resource
}

// NewResourceBuilder creates a new ResourceBuilder. This method should be called on the start of the application.
func NewResourceBuilder(rac ResourceAttributesConfig) *ResourceBuilder {
	return &ResourceBuilder{
		config: rac,
		res:    pcommon.NewResource(),
	}
}

// SetVcenterClusterName sets provided value as "vcenter.cluster.name" attribute.
func (rb *ResourceBuilder) SetVcenterClusterName(val string) {
	if rb.config.VcenterClusterName.Enabled {
		rb.res.Attributes().PutStr("vcenter.cluster.name", val)
	}
}

// SetVcenterDatastoreName sets provided value as "vcenter.datastore.name" attribute.
func (rb *ResourceBuilder) SetVcenterDatastoreName(val string) {
	if rb.config.VcenterDatastoreName.Enabled {
		rb.res.Attributes().PutStr("vcenter.datastore.name", val)
	}
}

// SetVcenterHostName sets provided value as "vcenter.host.name" attribute.
func (rb *ResourceBuilder) SetVcenterHostName(val string) {
	if rb.config.VcenterHostName.Enabled {
		rb.res.Attributes().PutStr("vcenter.host.name", val)
	}
}

// SetVcenterResourcePoolName sets provided value as "vcenter.resource_pool.name" attribute.
func (rb *ResourceBuilder) SetVcenterResourcePoolName(val string) {
	if rb.config.VcenterResourcePoolName.Enabled {
		rb.res.Attributes().PutStr("vcenter.resource_pool.name", val)
	}
}

// SetVcenterVMID sets provided value as "vcenter.vm.id" attribute.
func (rb *ResourceBuilder) SetVcenterVMID(val string) {
	if rb.config.VcenterVMID.Enabled {
		rb.res.Attributes().PutStr("vcenter.vm.id", val)
	}
}

// SetVcenterVMName sets provided value as "vcenter.vm.name" attribute.
func (rb *ResourceBuilder) SetVcenterVMName(val string) {
	if rb.config.VcenterVMName.Enabled {
		rb.res.Attributes().PutStr("vcenter.vm.name", val)
	}
}

// Emit returns the built resource and resets the internal builder state.
func (rb *ResourceBuilder) Emit() pcommon.Resource {
	r := rb.res
	rb.res = pcommon.NewResource()
	return r
}
