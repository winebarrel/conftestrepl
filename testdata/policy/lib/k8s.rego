package lib.k8s

is_deployment if input.kind == "Deployment"

name := input.metadata.name
