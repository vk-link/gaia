/*
Copyright The Gaia Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Code generated by informer-gen. DO NOT EDIT.

package v1alpha1

import (
	internalinterfaces "github.com/lmxia/gaia/pkg/generated/informers/externalversions/internalinterfaces"
)

// Interface provides access to all the informers in this group version.
type Interface interface {
	// Descriptions returns a DescriptionInformer.
	Descriptions() DescriptionInformer
	// NetworkRequirements returns a NetworkRequirementInformer.
	NetworkRequirements() NetworkRequirementInformer
	// ResourceBindings returns a ResourceBindingInformer.
	ResourceBindings() ResourceBindingInformer
}

type version struct {
	factory          internalinterfaces.SharedInformerFactory
	namespace        string
	tweakListOptions internalinterfaces.TweakListOptionsFunc
}

// New returns a new Interface.
func New(f internalinterfaces.SharedInformerFactory, namespace string, tweakListOptions internalinterfaces.TweakListOptionsFunc) Interface {
	return &version{factory: f, namespace: namespace, tweakListOptions: tweakListOptions}
}

// Descriptions returns a DescriptionInformer.
func (v *version) Descriptions() DescriptionInformer {
	return &descriptionInformer{factory: v.factory, namespace: v.namespace, tweakListOptions: v.tweakListOptions}
}

// NetworkRequirements returns a NetworkRequirementInformer.
func (v *version) NetworkRequirements() NetworkRequirementInformer {
	return &networkRequirementInformer{factory: v.factory, namespace: v.namespace, tweakListOptions: v.tweakListOptions}
}

// ResourceBindings returns a ResourceBindingInformer.
func (v *version) ResourceBindings() ResourceBindingInformer {
	return &resourceBindingInformer{factory: v.factory, namespace: v.namespace, tweakListOptions: v.tweakListOptions}
}
