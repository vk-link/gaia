/*
Copyright 2021 The Clusternet Authors.

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

package rbmerger

import (
	"context"
	"fmt"
	appv1alpha1 "github.com/lmxia/gaia/pkg/apis/apps/v1alpha1"
	"github.com/lmxia/gaia/pkg/common"
	"github.com/lmxia/gaia/pkg/controllers/resourcebindingmerger"
	gaiaClientSet "github.com/lmxia/gaia/pkg/generated/clientset/versioned"
	gaiainformers "github.com/lmxia/gaia/pkg/generated/informers/externalversions"
	appsLister "github.com/lmxia/gaia/pkg/generated/listers/apps/v1alpha1"
	platLister "github.com/lmxia/gaia/pkg/generated/listers/platform/v1alpha1"
	"github.com/lmxia/gaia/pkg/utils"
	"github.com/lmxia/gaia/pkg/utils/cartesian"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

// RBMerger defines configuration for ResourceBindings approver
type RBMerger struct {
	// rbmController *resourcebindingmerger.Controller
	rbToLocalController  *resourcebindingmerger.Controller
	rbTOParentController *resourcebindingmerger.Controller

	localKubeClient          *kubernetes.Clientset
	localGaiaClient          *gaiaClientSet.Clientset
	loaclGaiaInformerFactory gaiainformers.SharedInformerFactory
	rbLister                 appsLister.ResourceBindingLister
	mclsLister               platLister.ManagedClusterLister

	selfClusterName string

	parentGaiaClient *gaiaClientSet.Clientset

	rbsOfParentRB       map[string]*RBsOfParentRB
	fieldsRBsOfParentRB map[string]*FieldsRBs
	parentsRBsOfAPPid   map[string][]*string
}

// RBsOfParentRB contains all RB from mCls in a parentRB
type RBsOfParentRB struct {
	count         int
	rbNames       []*string
	rbsOfParentRB []*appv1alpha1.ResourceBindingApps
}

// FieldsRBs contains all RB from mCls in a parentRB
type FieldsRBs struct {
	countCls        int
	NamesOfFiledRBs []*string
	rbsOfFields     []*RBsOfParentRB
}

// NewRBMerger returns a new RBMerger for ResourceBinding.
func NewRBMerger(kubeclient *kubernetes.Clientset, gaiaclient *gaiaClientSet.Clientset,
		gaiaInformerFactory gaiainformers.SharedInformerFactory) (*RBMerger, error) {
	rbMerger := &RBMerger{
		localKubeClient:          kubeclient,
		localGaiaClient:          gaiaclient,
		loaclGaiaInformerFactory: gaiaInformerFactory,
		rbLister:                 gaiaInformerFactory.Apps().V1alpha1().ResourceBindings().Lister(),
		mclsLister:               gaiaInformerFactory.Platform().V1alpha1().ManagedClusters().Lister(),
		rbsOfParentRB:            make(map[string]*RBsOfParentRB),
		fieldsRBsOfParentRB:      make(map[string]*FieldsRBs),
		parentsRBsOfAPPid:        make(map[string][]*string),
	}

	rbLocalController, err := resourcebindingmerger.NewController(gaiaclient,
		gaiaInformerFactory.Apps().V1alpha1().ResourceBindings(),
		rbMerger.handleToLocalResourceBinding)
	if err != nil {
		return nil, err
	}
	rbMerger.rbToLocalController = rbLocalController

	return rbMerger, nil
}

func (rbMerger *RBMerger) RunToLocalResourceBindingMerger(threadiness int, stopCh <-chan struct{}) {
	klog.Info("Starting local ResourceBinding Merger ...")
	defer klog.Info("Shutting local ResourceBinding Merger ...")
	// todo: gorountine
	rbMerger.rbToLocalController.Run(threadiness, stopCh)
	return
}

func (rbMerger *RBMerger) RunToParentResourceBindingMerger(threadiness int, stopCh <-chan struct{}) {
	klog.Info("Starting parent ResourceBinding Merger ...")
	defer klog.Info("Shutting parent ResourceBinding Merger ...")
	// todo: gorountine
	rbMerger.rbTOParentController.Run(threadiness, stopCh)
	return
}

func (rbMerger *RBMerger) SetParentRBController() (*RBMerger, error) {
	parentGaiaClient, _, _ := utils.SetParentClient(rbMerger.localKubeClient, rbMerger.localGaiaClient)
	selfClusterName, _, errClusterName := utils.GetLocalClusterName(rbMerger.localKubeClient)
	if errClusterName != nil {
		klog.Errorf("local handleResourceBinding failed to get clustername From secret: %v", errClusterName)
		return nil, errClusterName
	}
	rbMerger.parentGaiaClient = parentGaiaClient
	rbMerger.selfClusterName = selfClusterName

	rbTOParentController, err := resourcebindingmerger.NewController(rbMerger.localGaiaClient,
		rbMerger.loaclGaiaInformerFactory.Apps().V1alpha1().ResourceBindings(),
		rbMerger.handleToParentResourceBinding)
	if err != nil {
		return nil, err
	}
	rbMerger.rbTOParentController = rbTOParentController
	return rbMerger, nil
}

func (rbMerger *RBMerger) handleToParentResourceBinding(rb *appv1alpha1.ResourceBinding) error {
	klog.V(5).Infof("handle local resourceBinding %s", klog.KObj(rb))

	clusters, err := rbMerger.mclsLister.List(labels.Everything())
	if err != nil {
		klog.Warningf("failed to list managed clusters: %v", err)
	}

	if rb.Namespace != common.GaiaRSToBeMergedReservedNamespace {
		klog.V(4).Infof("The ResourceBinding not in %q namespace.", common.GaiaRSToBeMergedReservedNamespace)
		return nil
	}
	if rb.Spec.StatusScheduler != "" {
		klog.V(4).Infof("ResourceBinding %q has already been processed with Result %q. Skip it.", klog.KObj(rb), rb.Status.Status)
		return nil
	}

	if len(clusters) == 0 {

		err := rbMerger.reCreateRBtoParent(rb)
		if err != nil {
			klog.V(4).Infof("Failed to create ResourceBinding %q to parent.", rb.Name, err)
			return err
		}

		err = rbMerger.localGaiaClient.AppsV1alpha1().ResourceBindings(common.GaiaRSToBeMergedReservedNamespace).Delete(context.TODO(), rb.Name, metav1.DeleteOptions{})
		if err != nil {
			klog.V(4).Infof("Resource Binding %q failed to delete. error: ", rb.Name, err)
			return err
		}

		return nil

	} else if rbMerger.parentGaiaClient != nil {

		if rbMerger.rbsOfParentRB[rb.Spec.ParentRB] == nil {
			rbMerger.rbsOfParentRB[rb.Spec.ParentRB] = &RBsOfParentRB{}
		}
		for _, value := range rb.Spec.RbApps {
			if value.Children != nil {
				rbMerger.rbsOfParentRB[rb.Spec.ParentRB].count = rb.Spec.TotalPeer
				rbMerger.rbsOfParentRB[rb.Spec.ParentRB].rbNames = append(rbMerger.rbsOfParentRB[rb.Spec.ParentRB].rbNames, &rb.Name)
				rbMerger.rbsOfParentRB[rb.Spec.ParentRB].rbsOfParentRB = append(rbMerger.rbsOfParentRB[rb.Spec.ParentRB].rbsOfParentRB, value)
			}
		}

		if rb.Spec.TotalPeer != 0 && len(rbMerger.rbsOfParentRB[rb.Spec.ParentRB].rbsOfParentRB) == rb.Spec.TotalPeer {
			if rbMerger.createCollectedRBs(rb) {
				err := rbMerger.deleteRBsCollected(rbMerger.rbsOfParentRB[rb.Spec.ParentRB].rbNames)
				if err != nil {
					klog.Infof("Successful created RB from %q, but failed to delete RBs locally.", rb.Spec.ParentRB)
					return err
				}
			}
		}
	}

	return nil
}

func (rbMerger *RBMerger) handleToLocalResourceBinding(rb *appv1alpha1.ResourceBinding) error {
	klog.V(5).Infof("handle local resourceBinding %s", klog.KObj(rb))

	if rb.Namespace != common.GaiaRSToBeMergedReservedNamespace && rb.Namespace != common.GaiaRBMergedReservedNamespace {
		return nil
	}
	// delete unselected RBs
	if rb.Namespace == common.GaiaRBMergedReservedNamespace && rb.Spec.StatusScheduler == appv1alpha1.ResourceBindingSelected {
		err := rbMerger.deleteRBsUnselected(rb)
		if err != nil {
			return err
		}
		return nil
	}

	if rb.Namespace == common.GaiaRSToBeMergedReservedNamespace && rb.Spec.StatusScheduler == appv1alpha1.ResourceBindingMerging {

		clusters, err := rbMerger.mclsLister.List(labels.Everything())
		if err != nil {
			klog.Warningf("failed to list managed clusters: %v", err)
		}

		// var fieldRBs *FieldRBs
		chanResult := make(chan []*appv1alpha1.ResourceBindingApps)
		// rbsOfParentRB[field-xx]RBs
		if rbMerger.rbsOfParentRB[rb.Name] == nil {
			rbMerger.rbsOfParentRB[rb.Name] = &RBsOfParentRB{}
		}

		rbMerger.rbsOfParentRB[rb.Name].count = len(rb.Spec.RbApps)
		for _, rbApp := range rb.Spec.RbApps {
			rbMerger.rbsOfParentRB[rb.Name].rbsOfParentRB = append(rbMerger.rbsOfParentRB[rb.Name].rbsOfParentRB, rbApp.Children[0])
		}

		if rbMerger.fieldsRBsOfParentRB[rb.Spec.ParentRB] == nil {
			rbMerger.fieldsRBsOfParentRB[rb.Spec.ParentRB] = &FieldsRBs{}
		}
		rbMerger.fieldsRBsOfParentRB[rb.Spec.ParentRB].countCls = len(clusters)
		rbMerger.fieldsRBsOfParentRB[rb.Spec.ParentRB].NamesOfFiledRBs = append(rbMerger.fieldsRBsOfParentRB[rb.Spec.ParentRB].NamesOfFiledRBs, &rb.Name)
		rbMerger.fieldsRBsOfParentRB[rb.Spec.ParentRB].rbsOfFields = append(rbMerger.fieldsRBsOfParentRB[rb.Spec.ParentRB].rbsOfFields, rbMerger.rbsOfParentRB[rb.Name])

		rbMerger.parentsRBsOfAPPid[rb.Spec.AppID] = append(rbMerger.parentsRBsOfAPPid[rb.Spec.AppID], &rb.Spec.ParentRB)

		if rbMerger.mergeResourceBinding(rb.Spec.ParentRB, rbMerger.fieldsRBsOfParentRB, chanResult, rb) {
			err := rbMerger.deleteRBsCollected(rbMerger.fieldsRBsOfParentRB[rb.Spec.ParentRB].NamesOfFiledRBs)
			if err != nil {
				klog.Infof("Successful merged RB from %q, but failed to delete RBs locally.", rb.Spec.ParentRB)
				return err
			}
		}

	}

	// when all parentRB of appid created, push

	return nil
}

func (rbMerger *RBMerger) mergeResourceBinding(parentRBName string, fieldsRBsOfParentRB map[string]*FieldsRBs, chanResult chan []*appv1alpha1.ResourceBindingApps, rb *appv1alpha1.ResourceBinding) bool {

	var childrens [][]*appv1alpha1.ResourceBindingApps
	if fieldsRbs, ok := fieldsRBsOfParentRB[parentRBName]; ok {
		if fieldsRbs.countCls == len(fieldsRbs.rbsOfFields) {
			for _, filedRBs := range fieldsRbs.rbsOfFields {
				childrens = append(childrens, filedRBs.rbsOfParentRB)
			}

			chanResult = cartesian.Iter(childrens...)

			// deploy the Merged ResourceBinding
			rbMerger.getMergedResourceBindings(chanResult, &parentRBName, rb)
		}
		return false
	}
	return true
}

func (rbMerger *RBMerger) getMergedResourceBindings(chanResult chan []*appv1alpha1.ResourceBindingApps, parentRBName *string, rb *appv1alpha1.ResourceBinding) {
	// deploy the Merged ResourceBinding
	index := 0

	for rbN := range chanResult {
		// create new result ResourceBinding
		newResultRB := &appv1alpha1.ResourceBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-%d", *parentRBName, index),
				Namespace: common.GaiaRBMergedReservedNamespace,
				Labels: map[string]string{
					common.TotalPeerOfParentRB:  fmt.Sprintf("%d", rb.Spec.TotalPeer),
					common.GaiaDescriptionLabel: rb.Labels[common.GaiaDescriptionLabel],
				},
			},
			Spec: appv1alpha1.ResourceBindingSpec{
				AppID:           rb.Spec.AppID,
				RbApps:          rbN,
				StatusScheduler: appv1alpha1.ResourceBindingmerged,
			},
		}
		newResultRB.Kind = "ResourceBinding"
		newResultRB.APIVersion = "apps.gaia.io/v1alpha1"

		_, err := rbMerger.localGaiaClient.AppsV1alpha1().ResourceBindings(common.GaiaRBMergedReservedNamespace).Create(context.TODO(), newResultRB, metav1.CreateOptions{})
		if err != nil {
			klog.V(3).InfoS("ResourceBinding of %q merge success, but not created success %q.", *parentRBName, common.GaiaRSToBeMergedReservedNamespace, err)
		}

		index += 1
		// limit amount
		if index > 1 {
			break
		}
	}

}

func (rbMerger *RBMerger) createCollectedRBs(rb *appv1alpha1.ResourceBinding) bool {

	if len(rbMerger.rbsOfParentRB[rb.Spec.ParentRB].rbsOfParentRB) == 0 {
		return false
	}

	var rbApps []*appv1alpha1.ResourceBindingApps
	for index, rbAppChild := range rbMerger.rbsOfParentRB[rb.Spec.ParentRB].rbsOfParentRB {
		rbApp := &appv1alpha1.ResourceBindingApps{
			ClusterName: *rbMerger.rbsOfParentRB[rb.Spec.ParentRB].rbNames[index],
			Children:    []*appv1alpha1.ResourceBindingApps{rbAppChild},
		}

		rbApps = append(rbApps, rbApp)
	}

	// totalPeer, err := strconv.Atoi(rb.Labels[common.TotalPeerOfParentRB])
	// if err != nil {
	// 	klog.V(5).Infof("Failed to get totalPeer from label.")
	// 	totalPeer = 0
	// }
	totalPeer := 2
	// create new result ResourceBinding in parent cluster
	newResultRB := &appv1alpha1.ResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s", rb.Spec.ParentRB, rbMerger.selfClusterName),
			Namespace: common.GaiaRSToBeMergedReservedNamespace,
			Labels: map[string]string{
				common.GaiaDescriptionLabel: rb.GetLabels()[common.GaiaDescriptionLabel],
			},
		},
		Spec: appv1alpha1.ResourceBindingSpec{
			AppID:           rb.Spec.AppID,
			TotalPeer:       totalPeer,
			ParentRB:        rb.Spec.ParentRB,
			RbApps:          rbApps,
			StatusScheduler: appv1alpha1.ResourceBindingMerging,
		},
	}
	newResultRB.Kind = "ResourceBinding"
	newResultRB.APIVersion = "apps.gaia.io/v1alpha1"

	_, err := rbMerger.parentGaiaClient.AppsV1alpha1().ResourceBindings(common.GaiaRSToBeMergedReservedNamespace).Create(context.TODO(), newResultRB, metav1.CreateOptions{})
	if err != nil {
		klog.V(4).InfoS("Failed to create ResourceBinding  %q.", newResultRB.Name, err)
		return false
	}
	return true
}

func (rbMerger *RBMerger) deleteRBsUnselected(rb *appv1alpha1.ResourceBinding) error {
	klog.V(4).Infof("Delete unselected RBs of desc %q.", common.GaiaRSToBeMergedReservedNamespace)

	rb.Labels[common.StatusScheduler] = string(appv1alpha1.ResourceBindingSelected)
	_, err := rbMerger.localGaiaClient.AppsV1alpha1().ResourceBindings(common.GaiaRBMergedReservedNamespace).Update(context.TODO(), rb, metav1.UpdateOptions{})
	if err != nil {
		klog.V(4).Infof("Failed to update RB %q.", rb.Name)
		return err
	}

	descName := rb.GetLabels()[common.GaiaDescriptionLabel]
	err = rbMerger.localGaiaClient.AppsV1alpha1().ResourceBindings(common.GaiaRBMergedReservedNamespace).
			DeleteCollection(context.TODO(), metav1.DeleteOptions{}, metav1.ListOptions{LabelSelector: labels.SelectorFromSet(labels.Set{
				common.GaiaDescriptionLabel: descName,
				common.StatusScheduler:      string(appv1alpha1.ResourceBindingmerged),
			}).String()})
	if err != nil {
		klog.Infof("failed to delete rbs in %s namespace", common.GaiaRBMergedReservedNamespace, err)
		return err
	}
	return nil
}

func (rbMerger *RBMerger) deleteRBsCollected(rbNames []*string) error {
	for _, name := range rbNames {
		err := rbMerger.localGaiaClient.AppsV1alpha1().ResourceBindings(common.GaiaRSToBeMergedReservedNamespace).Delete(context.TODO(), *name, metav1.DeleteOptions{})
		if err != nil {
			klog.Infof("failed to delete rb %q in %s namespace", *name, common.GaiaRSToBeMergedReservedNamespace, err)
			return err
		}
	}
	return nil
}

func (rbMerger *RBMerger) reCreateRBtoParent(rb *appv1alpha1.ResourceBinding) error {
	newRB := &appv1alpha1.ResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rb.Name,
			Namespace: common.GaiaRSToBeMergedReservedNamespace,
			Labels:    rb.Labels,
		},
		Spec: rb.Spec,
	}
	rb.Kind = "ResourceBinding"
	rb.APIVersion = "apps.gaia.io/v1alpha1"

	_, err := rbMerger.parentGaiaClient.AppsV1alpha1().ResourceBindings(common.GaiaRSToBeMergedReservedNamespace).Create(context.TODO(), newRB, metav1.CreateOptions{})

	return err
}
