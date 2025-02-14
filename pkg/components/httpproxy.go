package components

import (
	"context"
	ytv1 "github.com/ytsaurus/yt-k8s-operator/api/v1"
	"github.com/ytsaurus/yt-k8s-operator/pkg/apiproxy"
	"github.com/ytsaurus/yt-k8s-operator/pkg/consts"
	"github.com/ytsaurus/yt-k8s-operator/pkg/labeller"
	"github.com/ytsaurus/yt-k8s-operator/pkg/resources"
	"github.com/ytsaurus/yt-k8s-operator/pkg/ytconfig"
	"go.ytsaurus.tech/yt/go/yt"
	corev1 "k8s.io/api/core/v1"
)

type httpProxy struct {
	componentBase
	server server

	serviceType      corev1.ServiceType
	master           Component
	balancingService *resources.HTTPService

	role        string
	httpsSecret *resources.TLSSecret

	ytClient yt.Client
}

func NewHTTPProxy(
	cfgen *ytconfig.Generator,
	ytsaurus *apiproxy.Ytsaurus,
	masterReconciler Component,
	spec ytv1.HTTPProxiesSpec) Component {

	resource := ytsaurus.GetResource()
	l := labeller.Labeller{
		ObjectMeta:     &resource.ObjectMeta,
		APIProxy:       ytsaurus.APIProxy(),
		ComponentLabel: cfgen.FormatComponentStringWithDefault(consts.YTComponentLabelHTTPProxy, spec.Role),
		ComponentName:  cfgen.FormatComponentStringWithDefault("HttpProxy", spec.Role),
		MonitoringPort: consts.HTTPProxyMonitoringPort,
	}

	server := newServer(
		&l,
		ytsaurus,
		&spec.InstanceSpec,
		"/usr/bin/ytserver-http-proxy",
		"ytserver-http-proxy.yson",
		cfgen.GetHTTPProxiesStatefulSetName(spec.Role),
		cfgen.GetHTTPProxiesHeadlessServiceName(spec.Role),
		func() ([]byte, error) {
			return cfgen.GetHTTPProxyConfig(spec)
		},
	)

	var httpsSecret *resources.TLSSecret
	if spec.Transport.HTTPSSecret != nil {
		httpsSecret = resources.NewTLSSecret(
			spec.Transport.HTTPSSecret.Name,
			consts.HTTPSSecretVolumeName,
			consts.HTTPSSecretMountPoint)
	}

	return &httpProxy{
		componentBase: componentBase{
			labeller: &l,
			ytsaurus: ytsaurus,
			cfgen:    cfgen,
		},
		server:      server,
		master:      masterReconciler,
		serviceType: spec.ServiceType,
		role:        spec.Role,
		httpsSecret: httpsSecret,
		balancingService: resources.NewHTTPService(
			cfgen.GetHTTPProxiesServiceName(spec.Role),
			&spec.Transport,
			&l,
			ytsaurus.APIProxy()),
	}
}

func (hp *httpProxy) IsUpdatable() bool {
	return true
}

func (hp *httpProxy) Fetch(ctx context.Context) error {
	return resources.Fetch(ctx, []resources.Fetchable{
		hp.server,
		hp.balancingService,
	})
}

func (hp *httpProxy) doSync(ctx context.Context, dry bool) (ComponentStatus, error) {
	var err error

	if hp.ytsaurus.GetClusterState() == ytv1.ClusterStateRunning && hp.server.needUpdate() {
		return SimpleStatus(SyncStatusNeedLocalUpdate), err
	}

	if hp.ytsaurus.GetClusterState() == ytv1.ClusterStateUpdating {
		if status, err := handleUpdatingClusterState(ctx, hp.ytsaurus, hp, &hp.componentBase, hp.server, dry); status != nil {
			return *status, err
		}
	}

	if !(hp.master.Status(ctx).SyncStatus == SyncStatusReady) {
		return WaitingStatus(SyncStatusBlocked, hp.master.GetName()), err
	}

	if hp.server.needSync() {
		if !dry {
			statefulSet := hp.server.buildStatefulSet()
			if hp.httpsSecret != nil {
				hp.httpsSecret.AddVolume(&statefulSet.Spec.Template.Spec)
				hp.httpsSecret.AddVolumeMount(&statefulSet.Spec.Template.Spec.Containers[0])
			}
			err = hp.server.Sync(ctx)
		}
		return WaitingStatus(SyncStatusPending, "components"), err
	}

	if !resources.Exists(hp.balancingService) {
		if !dry {
			s := hp.balancingService.Build()
			s.Spec.Type = hp.serviceType
			err = hp.balancingService.Sync(ctx)
		}
		return WaitingStatus(SyncStatusPending, hp.balancingService.Name()), err
	}

	if !hp.server.arePodsReady(ctx) {
		return WaitingStatus(SyncStatusBlocked, "pods"), err
	}

	return SimpleStatus(SyncStatusReady), err
}

func (hp *httpProxy) Status(ctx context.Context) ComponentStatus {
	status, err := hp.doSync(ctx, true)
	if err != nil {
		panic(err)
	}

	return status
}

func (hp *httpProxy) Sync(ctx context.Context) error {
	_, err := hp.doSync(ctx, false)
	return err
}
