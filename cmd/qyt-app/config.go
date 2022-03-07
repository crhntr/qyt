package main

import "os"

const (
	defaultFieldValueBranchRegex  = ".*"
	defaultFieldValueYQExpression = "."
	defaultFieldValueFileFilter   = `(.+)\.ya?ml`
)

func defaultFieldBranchRegex() string {
	e := os.Getenv("QYT_BRANCH_REGEX")
	if e != "" {
		return e
	}
	return defaultFieldValueBranchRegex
}
func defaultFieldFileFilter() string {
	e := os.Getenv("QYT_FILE_REGEX")
	if e != "" {
		return e
	}
	return defaultFieldValueFileFilter
}
func defaultFieldYQExpression() string {
	e := os.Getenv("QYT_YQ_EXPRESSION")
	if e != "" {
		return e
	}
	return defaultFieldValueYQExpression
}
