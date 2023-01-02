package reconciler

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"strings"

	"github.com/kyma-project/keda-manager/api/v1alpha1"
	"go.uber.org/zap"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	apirt "k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type stateFn func(context.Context, *fsm, *systemState) (stateFn, *ctrl.Result, error)

// module specific configuuration
type Cfg struct {
	// the Finalizer identifies the module and is is used to delete
	// the module resources
	Finalizer string
	// the objects are module component parts; objects are applied
	// on the cluster one by one with given order
	Objs []unstructured.Unstructured
}

var (
	fromUnstructured = apirt.DefaultUnstructuredConverter.FromUnstructured
	toUnstructed     = apirt.DefaultUnstructuredConverter.ToUnstructured
)

// updates given object by applying provided function with given data
func updateObj[T any, R any](u *unstructured.Unstructured, data R, update func(T, R) error) error {
	var result T
	err := fromUnstructured(u.Object, &result)
	if err != nil {
		return err
	}

	err = update(result, data)
	if err != nil {
		return err
	}

	u.Object, err = toUnstructed(&result)
	return err
}

func (c *Cfg) kedaOperatorDeployment() (*unstructured.Unstructured, error) {
	for i := range c.Objs {
		if !isKedaOperatorDeployment(c.Objs[i]) {
			continue
		}
		return &c.Objs[i], nil
	}
	return nil, fmt.Errorf("%w: operator object", ErrNotFound)
}

func updateKedaOperatorContainer0Args(deployment *appsv1.Deployment, logCfg v1alpha1.LoggingOperatorCfg) error {
	for i := range deployment.Spec.Template.Spec.Containers[0].Args {
		logCfg.UpdateArg(&deployment.Spec.Template.Spec.Containers[0].Args[i])
	}
	return nil
}

func (cfg *Cfg) updateOperatorLogging(logCfg v1alpha1.LoggingOperatorCfg) error {
	u, err := cfg.kedaOperatorDeployment()
	if err != nil {
		return err
	}
	return updateObj(u, logCfg, updateKedaOperatorContainer0Args)
}

// the state of controlled system (k8s cluster)
type systemState struct {
	instance v1alpha1.Keda
	// the state of module component parts on cluster used detect
	// module readiness
	objs []unstructured.Unstructured
}

const operatorName = "keda-manager"

type predicate func(unstructured.Unstructured) bool

var (
	ErrNotFound = errors.New("not found")

	hasOperatorName predicate = func(u unstructured.Unstructured) bool {
		return u.GetName() == operatorName
	}
	isDeployment predicate = func(u unstructured.Unstructured) bool {
		return u.GetKind() == "Deployment" &&
			u.GetAPIVersion() == "apps/v1"
	}
	isKedaOperatorDeployment predicate = func(u unstructured.Unstructured) bool {
		return hasOperatorName(u) && isDeployment(u)
	}
)

type K8s struct {
	client.Client
}

type Fsm interface {
	Run(ctx context.Context, v v1alpha1.Keda) (ctrl.Result, error)
}

type fsm struct {
	fn  stateFn
	log *zap.SugaredLogger
	K8s
	Cfg
}

func (m *fsm) stateFnName() string {
	fullName := runtime.FuncForPC(reflect.ValueOf(m.fn).Pointer()).Name()
	splitFullName := strings.Split(fullName, ".")

	if len(splitFullName) < 3 {
		return fullName
	}

	shortName := splitFullName[2]
	return shortName
}

func (m *fsm) Run(ctx context.Context, v v1alpha1.Keda) (ctrl.Result, error) {
	state := systemState{instance: v}
	var err error
	var result *ctrl.Result
loop:
	for m.fn != nil && err == nil {
		select {
		case <-ctx.Done():
			err = ctx.Err()
			break loop
		default:
			m.log.Info(fmt.Sprintf("switching state: %s", m.stateFnName()))
			m.fn, result, err = m.fn(ctx, m, &state)
		}
	}

	m.log.With("error", err).
		With("result", result).
		Info("reconciliation done")

	if result != nil {
		return *result, err
	}

	return ctrl.Result{
		Requeue: false,
	}, err
}

func NewFsm(ctx context.Context, log *zap.SugaredLogger, cfg Cfg, k8s K8s) Fsm {
	return &fsm{
		fn:  sFnInitialize,
		Cfg: cfg,
		log: log,
		K8s: k8s,
	}
}
