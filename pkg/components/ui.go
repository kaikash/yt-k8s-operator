package components

import (
	"context"
	"fmt"
	"path"
	"strings"

	ytv1 "github.com/ytsaurus/yt-k8s-operator/api/v1"
	"github.com/ytsaurus/yt-k8s-operator/pkg/apiproxy"
	"github.com/ytsaurus/yt-k8s-operator/pkg/consts"
	"github.com/ytsaurus/yt-k8s-operator/pkg/labeller"
	"github.com/ytsaurus/yt-k8s-operator/pkg/resources"
	"github.com/ytsaurus/yt-k8s-operator/pkg/ytconfig"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

type UI struct {
	componentBase
	microservice microservice
	initJob      *InitJob
	master       Component
	secret       *resources.StringSecret
}

const UIClustersConfigFileName = "clusters-config.json"
const UICustomConfigFileName = "common.js"

func NewUI(cfgen *ytconfig.Generator, ytsaurus *apiproxy.Ytsaurus, master Component) Component {
	r := ytsaurus.GetResource()
	l := labeller.Labeller{
		ObjectMeta:     &r.ObjectMeta,
		APIProxy:       ytsaurus.APIProxy(),
		ComponentLabel: consts.YTComponentLabelUI,
		ComponentName:  "UI",
	}

	image := r.Spec.UIImage
	if r.Spec.UI.Image != nil {
		image = *r.Spec.UI.Image
	}

	microservice := newMicroservice(
		&l,
		ytsaurus,
		image,
		r.Spec.UI.InstanceCount,
		map[string]ytconfig.GeneratorDescriptor{
			UIClustersConfigFileName: {
				F:   cfgen.GetUIClustersConfig,
				Fmt: ytconfig.ConfigFormatJson,
			},
			UICustomConfigFileName: {
				F:   cfgen.GetUICustomConfig,
				Fmt: ytconfig.ConfigFormatJsonWithJsPrologue,
			},
		},
		"ytsaurus-ui-deployment",
		"ytsaurus-ui")

	return &UI{
		componentBase: componentBase{
			labeller: &l,
			ytsaurus: ytsaurus,
			cfgen:    cfgen,
		},
		microservice: microservice,
		initJob: NewInitJob(
			&l,
			ytsaurus.APIProxy(),
			ytsaurus,
			r.Spec.ImagePullSecrets,
			"default",
			consts.ClientConfigFileName,
			r.Spec.CoreImage,
			cfgen.GetNativeClientConfig),
		secret: resources.NewStringSecret(
			l.GetSecretName(),
			&l,
			ytsaurus.APIProxy()),
		master: master,
	}
}

func (u *UI) IsUpdatable() bool {
	return true
}

func (u *UI) Fetch(ctx context.Context) error {

	return resources.Fetch(ctx, []resources.Fetchable{
		u.microservice,
		u.initJob,
		u.secret,
	})
}

func (u *UI) initUser() string {
	token, _ := u.secret.GetValue(consts.TokenSecretKey)
	commands := createUserCommand(consts.UIUserName, "", token, false)
	return strings.Join(commands, "\n")
}

func (u *UI) createInitScript() string {
	script := []string{
		initJobWithNativeDriverPrologue(),
		u.initUser(),
	}

	return strings.Join(script, "\n")
}

func (u *UI) syncComponents(ctx context.Context) (err error) {
	ytsaurusResource := u.ytsaurus.GetResource()
	service := u.microservice.buildService()
	service.Spec.Type = ytsaurusResource.Spec.UI.ServiceType

	volumeMounts := []corev1.VolumeMount{
		{
			Name:      consts.ConfigVolumeName,
			MountPath: path.Join(consts.UIClustersConfigMountPoint, UIClustersConfigFileName),
			SubPath:   UIClustersConfigFileName,
			ReadOnly:  true,
		},
		{
			Name:      consts.ConfigVolumeName,
			MountPath: path.Join(consts.UICustomConfigMountPoint, UICustomConfigFileName),
			SubPath:   UICustomConfigFileName,
			ReadOnly:  true,
		},
		{
			Name:      consts.UIVaultVolumeName,
			MountPath: consts.UIVaultMountPoint,
			ReadOnly:  true,
		},
		{
			Name:      consts.UISecretsVolumeName,
			MountPath: consts.UISecretsMountPoint,
			ReadOnly:  false,
		},
	}

	env := []corev1.EnvVar{
		{
			Name:  "YT_AUTH_CLUSTER_ID",
			Value: ytsaurusResource.Name,
		},
		{
			Name:  "APP_INSTALLATION",
			Value: "custom",
		},
	}

	if ytsaurusResource.Spec.UI.UseInsecureCookies {
		env = append(env, corev1.EnvVar{
			Name:  "YT_AUTH_ALLOW_INSECURE",
			Value: "1",
		})
	}

	env = append(env, ytsaurusResource.Spec.UI.ExtraEnvVariables...)

	secretsVolumeSize, _ := resource.ParseQuantity("1Mi")
	deployment := u.microservice.buildDeployment()
	deployment.Spec.Template.Spec.InitContainers = []corev1.Container{
		{
			Image: u.microservice.getImage(),
			Name:  consts.PrepareSecretContainerName,
			Command: []string{
				"bash",
				"-c",
				fmt.Sprintf("cp %s %s",
					path.Join(consts.UIVaultMountPoint, consts.UISecretFileName),
					consts.UISecretsMountPoint),
			},
			VolumeMounts: volumeMounts,
		},
	}

	deployment.Spec.Template.Spec.Containers = []corev1.Container{
		{
			Image:        u.microservice.getImage(),
			Name:         consts.UIContainerName,
			Env:          env,
			Command:      []string{"supervisord"},
			VolumeMounts: volumeMounts,
		},
	}

	deployment.Spec.Template.Spec.Volumes = []corev1.Volume{
		{
			Name: consts.ConfigVolumeName,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: u.labeller.GetMainConfigMapName(),
					},
				},
			},
		},
		{
			Name: consts.UIVaultVolumeName,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: u.secret.Name(),
				},
			},
		},
		{
			Name: consts.UISecretsVolumeName,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{
					SizeLimit: &secretsVolumeSize,
				},
			},
		},
	}

	return u.microservice.Sync(ctx)
}

func (u *UI) doSync(ctx context.Context, dry bool) (ComponentStatus, error) {
	var err error

	if u.ytsaurus.GetClusterState() == ytv1.ClusterStateRunning && u.microservice.needUpdate() {
		return SimpleStatus(SyncStatusNeedLocalUpdate), err
	}

	if u.ytsaurus.GetClusterState() == ytv1.ClusterStateUpdating {
		if IsUpdatingComponent(u.ytsaurus, u) {

			if u.ytsaurus.GetUpdateState() == ytv1.UpdateStateWaitingForPodsRemoval {
				if !dry {
					err = removePods(ctx, u.microservice, &u.componentBase)
				}
				return WaitingStatus(SyncStatusUpdating, "pods removal"), err
			}

			if u.ytsaurus.GetUpdateState() != ytv1.UpdateStateWaitingForPodsCreation {
				return NewComponentStatus(SyncStatusReady, "Nothing to do now"), err
			}
		} else {
			return NewComponentStatus(SyncStatusReady, "Not updating component"), err
		}
	}

	if u.master.Status(ctx).SyncStatus != SyncStatusReady {
		return WaitingStatus(SyncStatusBlocked, u.master.GetName()), err
	}

	if u.secret.NeedSync(consts.TokenSecretKey, "") {
		if !dry {
			token := ytconfig.RandString(30)
			s := u.secret.Build()
			s.StringData = map[string]string{
				consts.UISecretFileName: fmt.Sprintf("{\"oauthToken\" : \"%s\"}", token),
				consts.TokenSecretKey:   token,
			}
			err = u.secret.Sync(ctx)
		}
		return WaitingStatus(SyncStatusPending, u.secret.Name()), err
	}

	if !dry {
		u.initJob.SetInitScript(u.createInitScript())
	}
	status, err := u.initJob.Sync(ctx, dry)
	if err != nil || status.SyncStatus != SyncStatusReady {
		return status, err
	}

	if u.microservice.needSync() {
		if !dry {
			err = u.syncComponents(ctx)
		}
		return WaitingStatus(SyncStatusPending, "components"), err
	}

	if !u.microservice.arePodsReady(ctx) {
		return WaitingStatus(SyncStatusPending, "pods"), err
	}

	return SimpleStatus(SyncStatusReady), err
}

func (u *UI) Status(ctx context.Context) ComponentStatus {
	status, err := u.doSync(ctx, true)
	if err != nil {
		panic(err)
	}

	return status
}

func (u *UI) Sync(ctx context.Context) error {
	_, err := u.doSync(ctx, false)
	return err
}
