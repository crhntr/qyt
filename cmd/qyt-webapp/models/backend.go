package models

type Backend interface {
	InitialConfiguration() (Configuration, error)
	ListBranchNames() ([]string, error)
}
