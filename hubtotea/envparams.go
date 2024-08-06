package main

import (
	"log"
	"os"
	"strconv"
)

func GetEnvBool(key string, defValue bool) bool {
	value, ok := os.LookupEnv(key)
	if !ok {
		return defValue
	}
	return value == "true" || value == "1"
}

func GetEnvInt(key string, defValue int) int {
	value, ok := os.LookupEnv(key)
	if !ok {
		return defValue
	}
	num, err := strconv.Atoi(value)
	if err != nil {
		log.Printf("Error: %s environment variable must be an integer\n", key)
		os.Exit(1)
	}
	return num
}

func GetEnvStrict(key string) string {
	value, ok := os.LookupEnv(key)
	if !ok {
		log.Printf("Error: %s environment variable not set\n", key)
		os.Exit(1)
	}
	return value
}

func GetEnvOptional(key string) *string {
	value, ok := os.LookupEnv(key)
	if !ok {
		return nil
	}
	return &value
}
