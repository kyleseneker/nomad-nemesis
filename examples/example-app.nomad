job "example-app" {
  datacenters = ["dc1"]
  type = "service"

  group "web" {
    count = 3 # Run 3 instances

    network {
      port "http" {}
    }

    task "server" {
      driver = "docker"

      config {
        image = "hashicorp/http-echo:latest"
        args = [
          "-listen=:{{env \"NOMAD_PORT_http\"}}",
          "-text=Hello from instance {{env \"NOMAD_ALLOC_INDEX\"}}"
        ]
        ports = ["http"]
      }

      resources {
        cpu    = 100 # MHz
        memory = 64  # MB
      }

      # service { ... } Block Removed for local testing without Consul
    }
  }
} 