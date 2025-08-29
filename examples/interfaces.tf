locals {
  myif_1 = {
    apiVersion = "interfaces.eda.nokia.com/v1alpha1"
    kind       = "Interface"
    metadata = {
      labels = {
        "eda.nokia.com/role" = "interSwitch"
      }
      name      = "leaf-1-ethernet-1-1"
      namespace = "eda"
    }
    spec = {
      description = "generated from terraform"
      enabled     = true
      lldp        = true
      members = [
        {
          enabled          = true
          interface        = "ethernet-1-1"
          lacpPortPriority = 32768
          node             = "leaf-1"
        },
      ]
      type = "interface"
    }
  }

  myif_2 = {
    apiVersion = "interfaces.eda.nokia.com/v1alpha1"
    kind       = "Interface"
    metadata = {
      labels = {
        "eda.nokia.com/role" = "interSwitch"
      }
      name      = "leaf-1-ethernet-1-2"
      namespace = "eda"
    }
    spec = {
      description = "generated from terraform"
      enabled     = true
      lldp        = true
      members = [
        {
          enabled          = true
          interface        = "ethernet-1-2"
          lacpPortPriority = 32768
          node             = "leaf-1"
        },
      ]
      type = "interface"
    }
  }

  interfaces = [{
    type = {
      create = {
        value = local.myif_1
      }
    }
    }, {
    type = {
      create = {
        value = local.myif_2
      }
    }
  }]
}
