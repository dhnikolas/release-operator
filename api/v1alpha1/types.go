package v1alpha1

const (
	BuildNameLabel = "build.release.salt.x5.ru/build-name"
)

type Repo struct {
	// +kubebuilder:validation:Pattern=`^(https?:\/\/)?([\da-z\.-]+)\.([a-z\.]{2,6})\/([\/\w\.-]){1,}\/?$`
	URL           string   `json:"URL"`
	AcceptFinalMR bool     `json:"acceptFinalMR,omitempty"`
	Branches      []Branch `json:"branches"`
}

type Branch struct {
	Name string `json:"name"`
}

func (r Repo) Equal(x Repo) bool {
	return r.URL == x.URL &&
		branchEqual(r.Branches, x.Branches)
}

func branchEqual(a, b []Branch) bool {
	if len(a) != len(b) {
		return false
	}
	for _, v := range a {
		isExist := false
		for _, branch := range b {
			if v.Name == branch.Name {
				isExist = true
			}
		}
		if !isExist {
			return false
		}
	}
	return true
}

type RepoStatus struct {
	URL                   string         `json:"URL"`
	BuildBranch           string         `json:"buildBranch"`
	ResolveConflictBranch string         `json:"resolveConflictBranch,omitempty"`
	Branches              []BranchStatus `json:"branches,omitempty"`
	FailureMessage        *string        `json:"failureMessage,omitempty"`
}

type BranchStatus struct {
	Name             string  `json:"name,omitempty"`
	IsMerged         bool    `json:"isMerged,omitempty"`
	IsConflict       bool    `json:"isConflict,omitempty"`
	IsValid          bool    `json:"isValid,omitempty"`
	Processed        bool    `json:"processed,omitempty"`
	MergeRequestID   string  `json:"mergeRequestID,omitempty"`
	MergeRequestName string  `json:"mergeRequestName,omitempty"`
	FailureMessage   *string `json:"failureMessage,omitempty"`
}

type ConflictState struct {
	RecreatedBuildBranch bool         `json:"recreatedBuildBranch,omitempty"`
	ResolveBranch        BranchStatus `json:"resolveBranch,omitempty"`
}
