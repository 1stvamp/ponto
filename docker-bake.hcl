variable "GO_VERSION" {
  default = "1.24"
}
variable "NODE_VERSION" {
  default = "20"
}
variable "TF_VERSION" {
  default = "1.5.5"
}
variable "REPO" {
  default = "ghcr.io/1stvamp/ponto"
}
variable "VERSION" {
  default = "edge"
}

target "_common" {
  args = {
    GO_VERSION   = GO_VERSION
    NODE_VERSION = NODE_VERSION
    TF_VERSION   = TF_VERSION
  }
  labels = {
    "org.opencontainers.image.title"       = "ponto"
    "org.opencontainers.image.description"  = "Interactive Terraform visualization. State and configuration explorer."
    "org.opencontainers.image.licenses"    = "MIT"
    "org.opencontainers.image.source"      = "https://github.com/1stvamp/ponto"
    "org.opencontainers.image.version"     = "${VERSION}"
  }
}

group "default" {
  targets = ["image-local"]
}

target "image-local" {
  inherits = ["_common"]
  target   = "standard"
  tags     = ["${REPO}:latest", "${REPO}:${VERSION}"]
  output   = ["type=docker"]
}

target "image-slim" {
  inherits = ["_common"]
  target   = "slim"
  tags     = ["${REPO}:slim", "${REPO}:slim-${VERSION}"]
  output   = ["type=docker"]
}

target "image-all" {
  inherits  = ["image-local"]
  platforms = ["linux/amd64", "linux/arm64"]
  output    = ["type=image"]
}

target "image-slim-all" {
  inherits  = ["image-slim"]
  platforms = ["linux/amd64", "linux/arm64"]
  output    = ["type=image"]
}
