package rdbc

import (
	"context"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	"os"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type RedisConfig struct {
	Username   string
	Password   string
	APIUrl     string
	Namespace  string
	CredSecret string
}

func (r *ReconcileRdbc) setRedisConfigs() (RedisConfig, error) {
	redisConfig := RedisConfig{}
	// Get Redis Credentials Secret name
	redisCredSecretName, err := GetRedisCredSecretName()
	if err != nil {
		log.Error(err, "Failed to get Redis Credential Secret")
		os.Exit(1)
	}
	// Get Redis Namespace
	redisNamespace, err := GetRedisNamespace()
	if err != nil {
		log.Error(err, "Failed to get Redis namespace")
		os.Exit(1)
	}
	// Get Redis API url
	redisServiceName, err := GetRedisApiUrl()
	if err != nil {
		log.Error(err, "Failed to get Redis API URL")
		os.Exit(1)
	}

	redisConfig.Namespace = redisNamespace
	redisConfig.CredSecret = redisCredSecretName
	redisConfig.APIUrl = redisServiceName
	// Set Redis Credentials
	err = r.setRedisCreds(&redisConfig)
	if err != nil {
		log.Error(err, "Failed to set Redis credentials configs")
		os.Exit(1)
	}

	return redisConfig, nil
}

func (r *ReconcileRdbc) setRedisCreds(redisConfig *RedisConfig) error {
	redisSecret := &corev1.Secret{}
	err := r.client.Get(
		context.TODO(),
		client.ObjectKey{Name: redisConfig.CredSecret, Namespace: redisConfig.Namespace},
		redisSecret)

	if err != nil {
		log.Error(err, "Failed to get Redis Credentials Secret")
		os.Exit(1)
	}
	if _, ok := redisSecret.Data["password"]; ok {
		redisConfig.Password = string(redisSecret.Data["password"])
	} else {
		return fmt.Errorf("failed to get a passwrod from secrets: %v in namespace: %v",
			redisConfig.CredSecret,
			redisConfig.Namespace)
	}
	if _, ok := redisSecret.Data["username"]; ok {
		redisConfig.Username = string(redisSecret.Data["username"])
	} else {
		return fmt.Errorf("failed to get a username from secrets: %v in namespace: %v",
			redisConfig.CredSecret,
			redisConfig.Namespace)
	}
	return nil
}


func GetRedisCredSecretName() (string, error) {
	redisCredSecret, found := os.LookupEnv(RedisCredSecret)
	if !found {
		return "", fmt.Errorf("%s must be set", RedisCredSecret)
	}
	return redisCredSecret, nil
}

func GetRedisNamespace() (string, error) {
	redisCredSecret, found := os.LookupEnv(RedisNS)
	if !found {
		return "", fmt.Errorf("%s must be set", RedisNS)
	}
	return redisCredSecret, nil
}

func GetRedisApiUrl() (string, error) {
	redisAPIUrl, found := os.LookupEnv(RedisAPI)
	if !found {
		return "", fmt.Errorf("%s must be set", RedisAPI)
	}
	return redisAPIUrl, nil
}
