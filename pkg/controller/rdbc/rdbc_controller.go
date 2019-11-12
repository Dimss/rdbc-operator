package rdbc

import (
	"context"
	"fmt"
	rdbcv1alpha1 "github.com/rdbc-operator/pkg/apis/rdbc/v1alpha1"
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

func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileRdbc{client: mgr.GetClient(), scheme: mgr.GetScheme()}
}

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

func (r *ReconcileRdbc) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling Rdbc")
	redis, err := r.setRedisConfigs()
	if err != nil {
		log.Error(err, "Failed to init Redis Configurations")
		os.Exit(1)
	}
	// Fetch the Rdbc
	rdbc := &rdbcv1alpha1.Rdbc{}
	err = r.client.Get(context.TODO(), request.NamespacedName, rdbc)
	if err != nil {
		if errors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}
	redisDb, err := r.initRedisDb(rdbc, redis)
	// Init finalizers
	err = r.initFinalization(rdbc, redis, redisDb)
	if err != nil {
		reqLogger.Error(err, "Failed to initialize finalizer")
		if err := r.updateRdbcStatus(fmt.Sprintf("%v", err), rdbc); err != nil {
			reqLogger.Error(err, "Failed to update CR status")
		}
		return reconcile.Result{}, err
	}

	if err != nil {
		reqLogger.Error(err, "Failed to init RedisDB")
		return reconcile.Result{}, err
	}
	dbExists, err := redis.CheckIfDbExists(redisDb.Uid)
	if err != nil {
		reqLogger.Error(err, "Failed to check if db already exists")
		return reconcile.Result{}, err
	}
	if dbExists {
		return r.syncCR(rdbc, redisDb)
	} else {
		if err := redis.CreateDb(redisDb); err != nil {
			reqLogger.Error(err, "unable create new db")
			return reconcile.Result{}, err
		}
		return r.syncCR(rdbc, redisDb)
	}

	return reconcile.Result{}, nil
}

func (r *ReconcileRdbc) syncCR(rdbc *rdbcv1alpha1.Rdbc, redisDb *RedisDb) (reconcile.Result, error) {
	rdbc.Spec.DbId = redisDb.Uid
	rdbc.Spec.Name = redisDb.Name
	rdbc.Spec.Size = redisDb.MemorySize
	if err := r.client.Update(context.TODO(), rdbc); err != nil {
		log.Error(err, "failed to update RDBC CR", "Name", rdbc.Name)
		return reconcile.Result{}, err
	}
	return reconcile.Result{}, nil
}

func (r *ReconcileRdbc) initRedisDb(rdbc *rdbcv1alpha1.Rdbc, redis *RedisConfig) (*RedisDb, error) {
	// It's a new DB
	if rdbc.Spec.DbId == 0 {
		db, err := NewRedisDb(rdbc.Spec.Name, rdbc.Spec.Size, rdbc.Spec.Password, redis)
		if err != nil {
			return nil, err
		}
		return db, nil
	} else {
		// Existing DB, fetch db details and sync into cluster
		db, err := redis.LoadRedisDb(rdbc.Spec.DbId)
		return db, err
	}
}

func (r *ReconcileRdbc) initFinalization(rdbc *rdbcv1alpha1.Rdbc, redis *RedisConfig, redisDb *RedisDb) error {
	isRdbcMarkedToBeDeleted := rdbc.GetDeletionTimestamp() != nil
	if isRdbcMarkedToBeDeleted {
		if contains(rdbc.GetFinalizers(), rdbcFinalizer) {
			if err := r.finalizeRdbc(redis, redisDb); err != nil {
				log.Error(err, "Failed to run finalizer")
				return err
			}
			rdbc.SetFinalizers(remove(rdbc.GetFinalizers(), rdbcFinalizer))
			err := r.client.Update(context.TODO(), rdbc)
			if err != nil {
				log.Error(err, "Failed to delete finalizer")
				return err
			}
		}
		return nil
	}

	if !contains(rdbc.GetFinalizers(), rdbcFinalizer) {
		if err := r.addFinalizer(rdbc); err != nil {
			log.Error(err, "Failed to add finalizer")
			return err
		}
	}
	return nil
}

func (r *ReconcileRdbc) finalizeRdbc(redis *RedisConfig, redisDb *RedisDb) error {
	err := redis.DeleteDb(redisDb)
	if err != nil {
		log.Error(err, "Failed to delete db at finalizer")
		return err
	}
	log.Info(fmt.Sprintf("Successfully finalized Rdbc: %s", redisDb.Name))
	return nil
}

func (r *ReconcileRdbc) addFinalizer(rdbc *rdbcv1alpha1.Rdbc) error {
	log.Info("Adding Finalizer for the Rdbc")
	rdbc.SetFinalizers(append(rdbc.GetFinalizers(), rdbcFinalizer))
	// Update CR
	err := r.client.Update(context.TODO(), rdbc)
	if err != nil {
		log.Error(err, "Failed to update Rdbc with finalizer")
		return err
	}
	return nil
}

func (r *ReconcileRdbc) updateRdbcStatus(message string, rdbc *rdbcv1alpha1.Rdbc) error {
	rdbc.Status.Message = message
	if err := r.client.Status().Update(context.TODO(), rdbc); err != nil {
		log.Error(err, "Failed to update CR status")
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
