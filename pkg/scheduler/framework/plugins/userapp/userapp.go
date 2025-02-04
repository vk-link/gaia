package userapp

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"

	"github.com/lmxia/gaia/pkg/apis/apps/v1alpha1"
	clusterapi "github.com/lmxia/gaia/pkg/apis/platform/v1alpha1"
	framework "github.com/lmxia/gaia/pkg/scheduler/framework/interfaces"
	"github.com/lmxia/gaia/pkg/scheduler/framework/plugins/names"
)

var _ framework.FilterPlugin = &UserAPP{}

// UserAPP is a plugin that checks if a component fit a cluster's sn.
type UserAPP struct {
	handle framework.Handle
}

func (a UserAPP) Name() string {
	return names.UserAPP
}

func (a UserAPP) Filter(ctx context.Context, com *v1alpha1.Component, cluster *clusterapi.ManagedCluster) *framework.Status {
	if cluster == nil {
		return framework.AsStatus(fmt.Errorf("invalid cluster"))
	}

	if com.Workload.Workloadtype == v1alpha1.WorkloadTypeUserApp {
		_, _, _, _, snMap, _, _ := cluster.GetHypernodeLabelsMapFromManagedCluster()
		if _, exist := snMap[com.Workload.TraitUserAPP.SN]; exist {
			return nil
		}
	} else {
		return nil
	}

	errReason := fmt.Sprintf("this cluster {%s}, has no sn {%s}", cluster.Name, com.Workload.TraitUserAPP.SN)
	return framework.NewStatus(framework.UnschedulableAndUnresolvable, errReason)
}

// New initializes a new plugin and returns it.
func New(_ runtime.Object, h framework.Handle) (framework.Plugin, error) {
	return &UserAPP{handle: h}, nil
}
