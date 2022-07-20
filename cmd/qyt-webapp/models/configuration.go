package models

type Configuration struct {
	Query                    string `env:"QYT_QUERY_EXPRESSION"  json:"query"                    flag:"q" default:"keys"                                           usage:"yq query expression it may be passed argument 1 after flags"`
	BranchFilter             string `env:"QYT_BRANCH_FILTER"     json:"branchFilter"             flag:"b" default:".*"                                             usage:"regular expression to filter branches"`
	FileNameFilter           string `env:"QYT_FILE_NAME_FILTER"  json:"fileNameFilter"           flag:"f" default:"(.+)\\.ya?ml"                                   usage:"regular expression to filter file paths it may be passed argument 2 after flags"`
	GitRepositoryPath        string `env:"QYT_REPO_PATH"         json:"gitRepositoryPath"        flag:"r" default:"."                                              usage:"path to git repository"`
	NewBranchPrefix          string `env:"QYT_NEW_BRANCH_PREFIX" json:"newBranchPrefix"          flag:"p" default:"qyt/"                                           usage:"prefix for new branches"`
	CommitToExistingBranches bool   `                            json:"commitToExistingBranches" flag:"o" default:"false"                                          usage:"commit to existing branches instead of new branches"`
	CommitTemplate           string `env:"QYT_COMMIT_TEMPLATE"   json:"commitTemplate"           flag:"m" default:"run yq {{printf \"%q\" .Query}} on {{.Branch}}" usage:"commit message template"`
}
