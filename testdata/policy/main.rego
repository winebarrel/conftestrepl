package main

import data.lib.k8s

deny contains msg if {
    k8s.is_deployment
    not k8s.name in data.allowed_names
    msg := sprintf("%s is not allowed", [k8s.name])
}
