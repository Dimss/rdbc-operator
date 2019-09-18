package rdbc

import (
	"context"
	"fmt"
	"github.com/go-logr/logr"
	rdbcv1alpha1 "github.com/rdbc-operator/pkg/apis/rdbc/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"os"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"

	"sigs.k8s.io/controller-runtime/pkg/source"
)

var log = logf.Log.WithName("controller_rdbc")

/**
* USER ACTION REQUIRED: This is a scaffold file intended for the user to modify with their own Controller
* business logic.  Delete these comments after modifying this file.*
 */

// Add creates a new Rdbc Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileRdbc{client: mgr.GetClient(), scheme: mgr.GetScheme()}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("rdbc-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource Rdbc
	err = c.Watch(&source.Kind{Type: &rdbcv1alpha1.Rdbc{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	// TODO(user): Modify this to be the types you create that are owned by the primary resource
	// Watch for changes to secondary resource Pods and requeue the owner Rdbc
	err = c.Watch(&source.Kind{Type: &corev1.Pod{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &rdbcv1alpha1.Rdbc{},
	})
	if err != nil {
		return err
	}

	return nil
}

// blank assignment to verify that ReconcileRdbc implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcileRdbc{}

// ReconcileRdbc reconciles a Rdbc object
type ReconcileRdbc struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client client.Client
	scheme *runtime.Scheme
}

// Reconcile reads that state of the cluster for a Rdbc object and makes changes based on the state read
// and what is in the Rdbc.Spec
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileRdbc) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling Rdbc")
	redisConfig, err := r.setRedisConfigs()
	if err != nil {
		log.Error(err, "Failed to init Redis Configurations")
		os.Exit(1)
	}
	reqLogger.Info(redisConfig.APIUrl)
	// Fetch the Rdbc
	rdbc := &rdbcv1alpha1.Rdbc{}
	err = r.client.Get(context.TODO(), request.NamespacedName, rdbc)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}
	// Init finalizers
	err = r.initFinalization(rdbc, redisConfig, reqLogger)
	if err != nil {
		reqLogger.Error(err, "Failed to initialize finalizer")
		return reconcile.Result{}, err
	}

	if rdbc.Status.DbEndpointUrl != "" {
		// All good, no changes requires
		return reconcile.Result{}, nil
	}
	if rdbc.Status.DbEndpointUrl == "" && rdbc.Status.DbUid != 0 {
		// Only fetch DB details and update CR endpoint
		err = r.getDb(rdbc, redisConfig)
		if err != nil {
			reqLogger.Error(err, "failed to create Redis DB")
			return reconcile.Result{}, err
		}
	} else {
		// Run entire loop,
		// Create DB
		err = r.createDb(rdbc, redisConfig)
		if err != nil {
			reqLogger.Error(err, "failed to create Redis DB")
			return reconcile.Result{}, err
		}
		// Fetch db details
		err = r.getDb(rdbc, redisConfig)
		if err != nil {
			reqLogger.Error(err, "failed to create Redis DB")
			return reconcile.Result{}, err
		}

	}
	return reconcile.Result{}, nil
}

func (r *ReconcileRdbc) createDb(rdbc *rdbcv1alpha1.Rdbc, redisConfig *RedisConfig) error {
	db := NewRedisDb(rdbc.Spec.Name, rdbc.Spec.Size, rdbc.Spec.Password)
	err := db.CreateDb(redisConfig)
	if err != nil {
		return fmt.Errorf("failed to create Redis DB")
	}
	rdbc.Status.DbUid = db.uid
	// Update CR object
	err = r.client.Status().Update(context.TODO(), rdbc)
	if err != nil {
		return fmt.Errorf("failed to update uid in Rdbc Status, db name: %v", db.Name)
	}
	return nil
}

func (r *ReconcileRdbc) getDb(rdbc *rdbcv1alpha1.Rdbc, redisConfig *RedisConfig) error {
	db := RedisDb{uid: rdbc.Status.DbUid}
	err := db.GetDb(redisConfig)
	if err != nil {
		return fmt.Errorf("failed to get Redis DB details")
	}
	rdbc.Status.DbEndpointUrl = db.endpoint
	err = r.client.Status().Update(context.TODO(), rdbc)
	if err != nil {
		return fmt.Errorf("failed to update endpoints in Rdbc Status, db name: %v", db.Name)
	}
	return nil
}

func (r *ReconcileRdbc) initFinalization(rdbc *rdbcv1alpha1.Rdbc, redisConfig *RedisConfig, reqLogger logr.Logger) error {
	isRdbcMarkedToBeDeleted := rdbc.GetDeletionTimestamp() != nil
	if isRdbcMarkedToBeDeleted {
		if contains(rdbc.GetFinalizers(), rdbcFinalizer) {
			// Run finalization logic for memcachedFinalizer. If the
			// finalization logic fails, don't remove the finalizer so
			// that we can retry during the next reconciliation.
			if err := r.finalizeRdbc(rdbc, redisConfig, reqLogger); err != nil {
				reqLogger.Error(err, "Failed to run finalizer")
				return err

			}

			// Remove rdbcFinalizer. Once all finalizers have been
			// removed, the object will be deleted.
			rdbc.SetFinalizers(remove(rdbc.GetFinalizers(), rdbcFinalizer))
			err := r.client.Update(context.TODO(), rdbc)
			if err != nil {
				reqLogger.Error(err, "Failed to delete finalizer")
				return err
			}
		}
		return nil
	}

	// Add finalizer for this CR
	if !contains(rdbc.GetFinalizers(), rdbcFinalizer) {
		if err := r.addFinalizer(reqLogger, rdbc); err != nil {
			reqLogger.Error(err, "Failed to add finalizer")
			return err
		}
	}
	return nil
}

func (r *ReconcileRdbc) finalizeRdbc(rdbc *rdbcv1alpha1.Rdbc, redisConfig *RedisConfig, reqLogger logr.Logger, ) error {
	db := RedisDb{uid: rdbc.Status.DbUid}
	err := db.DeleteDb(redisConfig)
	if err != nil {
		reqLogger.Error(err, "Failed to delete db at finalizer")
		return err
	}
	reqLogger.Info(fmt.Sprintf("Successfully finalized Rdbc: %s", rdbc.Name))
	return nil
}

func (r *ReconcileRdbc) addFinalizer(reqLogger logr.Logger, rdbc *rdbcv1alpha1.Rdbc) error {
	reqLogger.Info("Adding Finalizer for the Memcached")
	rdbc.SetFinalizers(append(rdbc.GetFinalizers(), rdbcFinalizer))

	// Update CR
	err := r.client.Update(context.TODO(), rdbc)
	if err != nil {
		reqLogger.Error(err, "Failed to update Rdbc with finalizer")
		return err
	}
	return nil
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func remove(list []string, s string) []string {
	for i, v := range list {
		if v == s {
			list = append(list[:i], list[i+1:]...)
		}
	}
	return list
}
