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
	"strconv"
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
	if err != nil {
		reqLogger.Error(err, "Failed to init RedisDB")
		return reconcile.Result{}, err
	}

	// Init finalizers
	isRdbcMarkedToBeDeleted, err := r.initFinalization(rdbc, redis, redisDb)
	if err != nil {
		reqLogger.Error(err, "Failed to initialize finalizer")
		if err := r.updateRdbcStatus(fmt.Sprintf("%v", err), rdbc); err != nil {
			reqLogger.Error(err, "Failed to update CR status")
		}
		return reconcile.Result{}, err
	}
	if isRdbcMarkedToBeDeleted {
		return reconcile.Result{}, err
	}

	dbExists, err := redis.CheckIfDbExists(redisDb.Uid)
	if err != nil {
		reqLogger.Error(err, "Failed to check if db already exists")
		return reconcile.Result{}, err
	}
	if dbExists {
		return r.syncCR(rdbc, redisDb, redis)
	} else {
		if err := redis.CreateDb(redisDb); err != nil {
			reqLogger.Error(err, "unable create new db")
			return reconcile.Result{}, err
		}
		return r.syncCR(rdbc, redisDb, redis)
	}

	return reconcile.Result{}, nil
}

func (r *ReconcileRdbc) syncCR(rdbc *rdbcv1alpha1.Rdbc, redisDb *RedisDb, redis *RedisConfig) (reconcile.Result, error) {
	newDb := false
	if _, ok := rdbc.ObjectMeta.Annotations["dbuid"]; !ok {
		newDb = true
	}
	rdbc.ObjectMeta.Annotations = map[string]string{"dbuid": fmt.Sprint(redisDb.Uid)}
	rdbc.Spec.Name = redisDb.Name
	rdbc.Spec.Size = redisDb.MemorySize
	// Once the spec is updated with valid redis db parameters update the CR in K8S
	// If for some reason, the update is failed, make sure that it's not a new db request
	// if it's new db request, remove the created db
	if err := r.client.Update(context.TODO(), rdbc); err != nil {
		log.Error(err, "failed to update RDBC CR", "Name", rdbc.Name)
		if newDb {
			log.Info(fmt.Sprintf("unable to update RDBC CR afeter new DB created, gonna remove new DB, dbid: %d", redisDb.Uid))
			// If wasn't able to delete new created db, we are fucked up!
			if err := redis.DeleteDb(redisDb); err != nil {
				log.Error(err, "Houston, we have a problem! Kill me now, or I'll destroy you Redis cluster, madafaka!")
				return reconcile.Result{}, err
			}
		}
		return reconcile.Result{}, err
	}
	return reconcile.Result{}, nil
}

func (r *ReconcileRdbc) initRedisDb(rdbc *rdbcv1alpha1.Rdbc, redis *RedisConfig) (*RedisDb, error) {

	if dbidValue, ok := rdbc.ObjectMeta.Annotations["dbuid"]; ok {
		// Existing DB, fetch db details and sync into cluster
		dbid, err := strconv.Atoi(dbidValue)
		if err != nil {
			log.Error(err, fmt.Sprintf("wasn't able to convert from string to int, dbid: %v", dbid))
			return nil, err
		}
		return redis.LoadRedisDb(int32(dbid))

	} else {
		// It's a new DB
		db, err := NewRedisDb(rdbc.Spec.Name, rdbc.Spec.Size, rdbc.Spec.Password, redis)
		if err != nil {
			return nil, err
		}
		return db, nil
	}

}

func (r *ReconcileRdbc) initFinalization(rdbc *rdbcv1alpha1.Rdbc, redis *RedisConfig, redisDb *RedisDb) (bool, error) {
	isRdbcMarkedToBeDeleted := rdbc.GetDeletionTimestamp() != nil
	if isRdbcMarkedToBeDeleted {
		if contains(rdbc.GetFinalizers(), rdbcFinalizer) {
			if err := r.finalizeRdbc(redis, redisDb); err != nil {
				log.Error(err, "Failed to run finalizer")
				return isRdbcMarkedToBeDeleted, err
			}
			rdbc.SetFinalizers(remove(rdbc.GetFinalizers(), rdbcFinalizer))
			err := r.client.Update(context.TODO(), rdbc)
			if err != nil {
				log.Error(err, "Failed to delete finalizer")
				return isRdbcMarkedToBeDeleted, err
			}
		}
		return isRdbcMarkedToBeDeleted, nil
	}

	if !contains(rdbc.GetFinalizers(), rdbcFinalizer) {
		if err := r.addFinalizer(rdbc); err != nil {
			log.Error(err, "Failed to add finalizer")
			return isRdbcMarkedToBeDeleted, err
		}
	}
	return isRdbcMarkedToBeDeleted, nil
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
