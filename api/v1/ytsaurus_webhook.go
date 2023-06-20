/*
Copyright 2023.

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

package v1

import (
	"fmt"

	"github.com/ytsaurus/yt-k8s-operator/pkg/consts"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

// log is for logging in this package.
var ytsauruslog = logf.Log.WithName("ytsaurus-resource")

func (r *Ytsaurus) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// TODO(user): EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

//+kubebuilder:webhook:path=/mutate-cluster-ytsaurus-tech-v1-ytsaurus,mutating=true,failurePolicy=fail,sideEffects=None,groups=cluster.ytsaurus.tech,resources=ytsaurus,verbs=create;update,versions=v1,name=mytsaurus.kb.io,admissionReviewVersions=v1

var _ webhook.Defaulter = &Ytsaurus{}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (r *Ytsaurus) Default() {
	ytsauruslog.Info("default", "name", r.Name)

	// Set anti affinity for masters
	if r.Spec.PrimaryMasters.Affinity == nil {
		r.Spec.PrimaryMasters.Affinity = &corev1.Affinity{}
	}
	if r.Spec.PrimaryMasters.Affinity.PodAntiAffinity == nil {
		r.Spec.PrimaryMasters.Affinity.PodAntiAffinity = &corev1.PodAntiAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
				{
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							consts.YTComponentLabelName: fmt.Sprintf("%s-%s", r.Name, "yt-master"),
						},
					},
					TopologyKey: "kubernetes.io/hostname",
				},
			},
		}
	}
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
//+kubebuilder:webhook:path=/validate-cluster-ytsaurus-tech-v1-ytsaurus,mutating=false,failurePolicy=fail,sideEffects=None,groups=cluster.ytsaurus.tech,resources=ytsaurus,verbs=create;update,versions=v1,name=vytsaurus.kb.io,admissionReviewVersions=v1

var _ webhook.Validator = &Ytsaurus{}

//////////////////////////////////////////////////

func (r *Ytsaurus) validateDiscovery() field.ErrorList {
	var allErrors field.ErrorList

	allErrors = append(allErrors, r.validateInstanceSpec(r.Spec.Discovery.InstanceSpec, field.NewPath("spec").Child("discovery"))...)

	return allErrors
}

func (r *Ytsaurus) validatePrimaryMasters() field.ErrorList {
	var allErrors field.ErrorList

	allErrors = append(allErrors, r.validateInstanceSpec(r.Spec.PrimaryMasters.InstanceSpec, field.NewPath("spec").Child("primaryMasters"))...)

	return allErrors
}

func (r *Ytsaurus) validateSecondaryMasters() field.ErrorList {
	var allErrors field.ErrorList

	for i, sm := range r.Spec.SecondaryMasters {
		path := field.NewPath("spec").Child("secondaryMasters").Index(i)
		allErrors = append(allErrors, r.validateInstanceSpec(sm.InstanceSpec, path)...)
	}

	return allErrors
}

func (r *Ytsaurus) validateHTTPProxies() field.ErrorList {
	var allErrors field.ErrorList

	httpRoles := make(map[string]bool)
	hasDefaultHTTPProxy := false
	for i, hp := range r.Spec.HTTPProxies {
		path := field.NewPath("spec").Child("httpProxies").Index(i)
		if _, exists := httpRoles[hp.Role]; exists {
			allErrors = append(allErrors, field.Duplicate(path.Child("role"), hp.Role))
		}
		if hp.Role == consts.DefaultHTTPProxyRole {
			hasDefaultHTTPProxy = true
		}
		httpRoles[hp.Role] = true

		allErrors = append(allErrors, r.validateInstanceSpec(hp.InstanceSpec, path)...)
	}

	if !hasDefaultHTTPProxy {
		allErrors = append(allErrors, field.Required(
			field.NewPath("spec").Child("httpProxies"),
			fmt.Sprintf("HTTP proxy with `%s` role should exist", consts.DefaultHTTPProxyRole)))
	}

	return allErrors
}

func (r *Ytsaurus) validateRPCProxies() field.ErrorList {
	var allErrors field.ErrorList

	rpcRoles := make(map[string]bool)
	for i, rp := range r.Spec.RPCProxies {
		path := field.NewPath("spec").Child("rpcProxies").Index(i)
		if _, exists := rpcRoles[rp.Role]; exists {
			allErrors = append(allErrors, field.Duplicate(path.Child("role"), rp.Role))
		}
		rpcRoles[rp.Role] = true

		allErrors = append(allErrors, r.validateInstanceSpec(rp.InstanceSpec, path)...)
	}

	return allErrors
}

func (r *Ytsaurus) validateDataNodes() field.ErrorList {
	var allErrors field.ErrorList

	for i, dn := range r.Spec.DataNodes {
		path := field.NewPath("spec").Child("dataNodes").Index(i)
		allErrors = append(allErrors, r.validateInstanceSpec(dn.InstanceSpec, path)...)
	}

	return allErrors
}

func (r *Ytsaurus) validateExecNodes() field.ErrorList {
	var allErrors field.ErrorList

	for i, en := range r.Spec.ExecNodes {
		path := field.NewPath("spec").Child("execNodes").Index(i)
		allErrors = append(allErrors, r.validateInstanceSpec(en.InstanceSpec, path)...)
	}

	return allErrors
}

func (r *Ytsaurus) validateSchedulers() field.ErrorList {
	var allErrors field.ErrorList

	if r.Spec.Schedulers != nil {
		path := field.NewPath("spec").Child("schedulers")
		allErrors = append(allErrors, r.validateInstanceSpec(r.Spec.Schedulers.InstanceSpec, path)...)
	}

	return allErrors
}

func (r *Ytsaurus) validateControllerAgents() field.ErrorList {
	var allErrors field.ErrorList

	if r.Spec.ControllerAgents != nil {
		path := field.NewPath("spec").Child("controllerAgents")
		allErrors = append(allErrors, r.validateInstanceSpec(r.Spec.ControllerAgents.InstanceSpec, path)...)
	}

	return allErrors
}

func (r *Ytsaurus) validateTabletNodes() field.ErrorList {
	var allErrors field.ErrorList

	for i, tn := range r.Spec.TabletNodes {
		path := field.NewPath("spec").Child("tabletNodes").Index(i)
		allErrors = append(allErrors, r.validateInstanceSpec(tn.InstanceSpec, path)...)
	}

	return allErrors
}

func (r *Ytsaurus) validateChyt() field.ErrorList {
	var allErrors field.ErrorList
	return allErrors
}

func (r *Ytsaurus) validateQueryTrackers() field.ErrorList {
	var allErrors field.ErrorList

	if r.Spec.QueryTrackers != nil {
		path := field.NewPath("spec").Child("queryTrackers")
		allErrors = append(allErrors, r.validateInstanceSpec(r.Spec.QueryTrackers.InstanceSpec, path)...)
	}

	return allErrors
}

func (r *Ytsaurus) validateSpyt() field.ErrorList {
	var allErrors field.ErrorList
	return allErrors
}

func (r *Ytsaurus) validateYQLAgents() field.ErrorList {
	var allErrors field.ErrorList

	if r.Spec.YQLAgents != nil {
		path := field.NewPath("spec").Child("YQLAgents")
		allErrors = append(allErrors, r.validateInstanceSpec(r.Spec.YQLAgents.InstanceSpec, path)...)
	}

	return allErrors
}

//////////////////////////////////////////////////

func (r *Ytsaurus) validateInstanceSpec(instanceSpec InstanceSpec, path *field.Path) field.ErrorList {
	var allErrors field.ErrorList

	if instanceSpec.EnableAntiAffinity != nil {
		allErrors = append(allErrors, field.Invalid(path.Child("EnableAntiAffinity"), instanceSpec.EnableAntiAffinity, "EnableAntiAffinity is deprecated, use Affinity instead"))
	}

	return allErrors
}

func (r *Ytsaurus) validateYtsaurus() field.ErrorList {
	var allErrors field.ErrorList

	allErrors = append(allErrors, r.validateDiscovery()...)
	allErrors = append(allErrors, r.validatePrimaryMasters()...)
	allErrors = append(allErrors, r.validateSecondaryMasters()...)
	allErrors = append(allErrors, r.validateHTTPProxies()...)
	allErrors = append(allErrors, r.validateRPCProxies()...)
	allErrors = append(allErrors, r.validateDataNodes()...)
	allErrors = append(allErrors, r.validateExecNodes()...)
	allErrors = append(allErrors, r.validateSchedulers()...)
	allErrors = append(allErrors, r.validateControllerAgents()...)
	allErrors = append(allErrors, r.validateTabletNodes()...)
	allErrors = append(allErrors, r.validateChyt()...)
	allErrors = append(allErrors, r.validateQueryTrackers()...)
	allErrors = append(allErrors, r.validateSpyt()...)
	allErrors = append(allErrors, r.validateYQLAgents()...)

	return allErrors
}

func (r *Ytsaurus) evaluateYtsaurusValidation() error {
	allErrors := r.validateYtsaurus()
	if len(allErrors) == 0 {
		return nil
	}

	return apierrors.NewInvalid(
		schema.GroupKind{Group: "cluster.ytsaurus.tech", Kind: "Ytsaurus"},
		r.Name,
		allErrors)
}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *Ytsaurus) ValidateCreate() error {
	ytsauruslog.Info("validate create", "name", r.Name)

	return r.evaluateYtsaurusValidation()
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *Ytsaurus) ValidateUpdate(old runtime.Object) error {
	ytsauruslog.Info("validate update", "name", r.Name)

	return r.evaluateYtsaurusValidation()
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *Ytsaurus) ValidateDelete() error {
	ytsauruslog.Info("validate delete", "name", r.Name)

	// TODO(user): fill in your validation logic upon object deletion.
	return nil
}
