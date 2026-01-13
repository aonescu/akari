package authority

import "strings"

type AuthorityMapping struct {
	Controller    string
	AllowedFields []string
}

var AuthorityTable = []AuthorityMapping{
	{
		Controller:    "kubelet",
		AllowedFields: []string{"status.conditions", "status.phase", "status.containerStatuses"},
	},
	{
		Controller:    "kube-scheduler",
		AllowedFields: []string{"spec.nodeName"},
	},
	{
		Controller:    "deployment-controller",
		AllowedFields: []string{"spec.replicas"},
	},
	{
		Controller:    "service-controller",
		AllowedFields: []string{"status.loadBalancer"},
	},
	{
		Controller:    "replicaset-controller",
		AllowedFields: []string{"spec.replicas"},
	},
	{
		Controller:    "node-controller",
		AllowedFields: []string{"status.conditions"},
	},
	{
		Controller:    "garbage-collector",
		AllowedFields: []string{"metadata.deletionTimestamp"},
	},
}

func GetAuthorizedControllers(field string) []string {
	var authorized []string
	for _, mapping := range AuthorityTable {
		for _, allowedField := range mapping.AllowedFields {
			if strings.HasPrefix(field, allowedField) {
				authorized = append(authorized, mapping.Controller)
				break
			}
		}
	}
	return authorized
}
