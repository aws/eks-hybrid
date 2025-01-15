packer {
  required_version = ">= 1.11.0"
  required_plugins {
    amazon = {
      version = ">= 1.2.8"
      source  = "github.com/hashicorp/amazon"
    }

    vsphere = {
      source  = "github.com/hashicorp/vsphere"
      version = ">= 1.4.0"
    }

    qemu = {
      source  = "github.com/hashicorp/qemu"
      version = "~> 1"
    }
  }
}

variable "credential_provider" {
  type        = string
  default     = env("CREDENTIAL_PROVIDER")
  description = "Authentication type for AWS temporary credentials using either SSM or IAM Anywhere, with SSM as default. Valid inputs are 'ssm' or 'iam'."

  validation {
    condition     = contains(["iam", "ssm"], var.credential_provider)
    error_message = "The CREDENTIAL_PROVIDER environment variable value must be either 'ssm' or 'iam'."
  }
}

variable "nodeadm_arch" {
  type        = string
  default     = env("NODEADM_ARCH")
  description = "Architecture for nodeadm install. Choose 'amd' or 'arm'."

  validation {
    condition     = length(var.nodeadm_arch) > 0
    error_message = "ERROR - NODEADM_ARCH environment variable is not set. Choose 'amd' or 'arm'."
  }
}

variable "aws_profile" {
  default     = env("AWS_PROFILE")
  description = "AWS profile for authentication. Set via local AWS_PROFILE environment variable."

  validation {
    condition     = length(var.aws_profile) > 0
    error_message = "ERROR - AWS_PROFILE environment variable is not set."
  }
}

variable "pkr_ssh_password" {
  default     = env("PKR_SSH_PASSWORD")
  description = "Password for Packer to SSH into the VM when provisioning. Have it match the password set in either the ks.cfg or user-data files for each OS."

  validation {
    condition     = length(var.pkr_ssh_password) > 0
    error_message = "ERROR - PKR_SSH_PASSWORD environment variable is not set. Make sure to set it in the corresponding ks.cfg or user-data files, too."
  }
}

####################
# ISO Image and Checksums
####################
variable "iso_url" {
  type        = string
  default     = env("ISO_URL")
  description = "URL to the RHEL ISO image. Set via local ISO_URL environment variable. Can be a server web link or an absolute path to a local file."

  validation {
    condition     = length(var.iso_url) > 0
    error_message = "ERROR - ISO_URL environment variable is not set."
  }
}

variable "iso_checksum" {
  type        = string
  default     = env("ISO_CHECKSUM")
  description = "Checksum of the RHEL ISO image. Set via local ISO_CHECKSUM environment variable."

  validation {
    condition     = length(var.iso_checksum) > 0
    error_message = "ERROR - ISO_CHECKSUM environment variable is not set."
  }
}

####################
# Qcow2/Raw output format variable with validator and version number
# required when using qemu builder
####################
variable "format" {
  type        = string
  default     = env("PACKER_OUTPUT_FORMAT")
  description = "Output format for the QEMU builder (qcow2, raw). Only required for the QEMU builder."

  validation {
    condition     = contains(["", "qcow2", "raw"], var.format)
    error_message = "The 'PACKER_OUTPUT_FORMAT environment variable must be set when using the QEMU builder. Set to qcow2 or raw."
  }
}

variable "rhel_version" {
  type        = string
  default     = env("RHEL_VERSION")
  description = "Rhel version of the input iso and output Qcow2/Raw image. Must be 8 or 9"

  validation {
    condition     = contains(["", "8", "9"], var.rhel_version)
    error_message = "The 'RHEL_VERSION' environment variable must be set when using the QEMU builder. Set to 8 or 9."
  }
}

####################
# Kubernetes version to install for nodeadm
####################
variable "k8s_version" {
  type        = string
  default     = env("K8S_VERSION")
  description = "Kubernetes version to use. Must be 1.26 - 1.31"

  validation {
    condition     = contains(["", "1.26", "1.27", "1.28", "1.29", "1.30", "1.31"], var.k8s_version)
    error_message = "The 'K8S_VERSION' environment variable must be set. Set any major version between 1.26 - 1.31."
  }
}

####################
# Rhel credentials
####################
variable "rhsm_username" {
  type        = string
  description = "RHEL Subscription Manager username"
  default     = env("RH_USERNAME")
}

variable "rhsm_password" {
  type        = string
  description = "RHEL Subscription Manager password"
  default     = env("RH_PASSWORD")
  sensitive   = true
}

####################
# vSphere variables
####################
variable "vsphere_server" {
  type    = string
  default = env("VSPHERE_SERVER")

}

variable "vsphere_user" {
  type    = string
  default = env("VSPHERE_USER")

}

variable "vsphere_password" {
  type      = string
  sensitive = true
  default   = env("VSPHERE_PASSWORD")

}

variable "vsphere_datacenter" {
  type    = string
  default = env("VSPHERE_DATACENTER")

}

variable "vsphere_cluster" {
  type    = string
  default = env("VSPHERE_CLUSTER")

}

variable "vsphere_datastore" {
  type    = string
  default = env("VSPHERE_DATASTORE")

}

variable "vsphere_network" {
  type    = string
  default = env("VSPHERE_NETWORK")

}

variable "vsphere_folder" {
  type    = string
  default = env("VSPHERE_OUTPUT_FOLDER")

}

locals {
  auth_value = var.credential_provider == "ssm" ? "ssm" : "iam-ra"
  k8s_release = var.k8s_version
  timestamp = formatdate("YYYY-MM-DD-hhmm", timestamp())
  qemu_output_directory = "qemu/${var.format}"
  rhel_os = var.rhel_version
  qemu_format = var.format
  iso_url = var.iso_url
  iso_checksum = var.iso_checksum
  nodeadm_link = "https://hybrid-assets.eks.amazonaws.com/latest/bin/linux/${var.nodeadm_arch}64/nodeadm"
}

######################
# Ubuntu AMI sources
######################
source "amazon-ebs" "ubuntu22" {
  ami_name      = "ami-packer-ubuntu22-${local.timestamp}"
  instance_type = "t2.micro"
  region        = "us-west-2"
  ssh_username  = "ubuntu"
  profile       = var.aws_profile

  source_ami_filter {
    filters = {
      name                = "ubuntu/images/*ubuntu-jammy-22.04-amd64-server-*"
      root-device-type    = "ebs"
      virtualization-type = "hvm"
    }
    most_recent = true
    owners      = ["099720109477"]
  }

}

source "amazon-ebs" "ubuntu24" {
  ami_name      = "ami-packer-ubuntu24-${local.timestamp}"
  instance_type = "t2.micro"
  region        = "us-west-2"
  ssh_username  = "ubuntu"
  profile       = var.aws_profile

  source_ami_filter {
    filters = {
      name                = "ubuntu/images/*ubuntu-noble-24.04-amd64-server-*"
      root-device-type    = "ebs"
      virtualization-type = "hvm"
    }
    most_recent = true
    owners      = ["099720109477"]
  }

}

######################
# Rhel AMI sources
######################
source "amazon-ebs" "rhel8" {
  ami_name      = "ami-packer-rhel8-${local.timestamp}"
  instance_type = "t2.micro"
  region        = "us-west-2"
  ssh_username  = "ec2-user"
  profile       = var.aws_profile

  source_ami_filter {
    filters = {
      name                = "RHEL-8.6.0_HVM-*"
      root-device-type    = "ebs"
      virtualization-type = "hvm"
    }
    most_recent = true
    owners      = ["309956199498"]
  }
}


source "amazon-ebs" "rhel9" {
  ami_name      = "ami-packer-rhel9-${local.timestamp}"
  instance_type = "t2.micro"
  region        = "us-west-2"
  ssh_username  = "ec2-user"
  profile       = var.aws_profile

  source_ami_filter {
    filters = {
      name                = "RHEL-9.2.0_HVM-*"
      root-device-type    = "ebs"
      virtualization-type = "hvm"
    }
    most_recent = true
    owners      = ["309956199498"]
  }
}


######################
# Ubuntu vSphere ISO sources
######################
source "vsphere-iso" "ubuntu22" {
  vcenter_server      = var.vsphere_server != "" ? var.vsphere_server : " "
  username            = var.vsphere_user != "" ? var.vsphere_user : " "
  password            = var.vsphere_password != "" ? var.vsphere_password : " "
  insecure_connection = true

  datacenter = var.vsphere_datacenter
  cluster    = var.vsphere_cluster != "" ? var.vsphere_cluster : " "
  datastore  = var.vsphere_datastore
  folder     = var.vsphere_folder

  vm_name              = "iso-packer-ubuntu22-${local.timestamp}"
  guest_os_type        = "ubuntu64Guest"
  CPUs                 = 4
  RAM                  = 16384
  disk_controller_type = ["pvscsi"]

  storage {
    disk_size             = 30000
    disk_thin_provisioned = true
  }

  network_adapters {
    network      = var.vsphere_network
    network_card = "vmxnet3"
  }

  boot_order = "disk,cdrom"

  cd_files = [

    "./http/meta-data",

    "./http/user-data"]

  cd_label = "cidata"

  iso_url      = local.iso_url
  iso_checksum = local.iso_checksum

  http_directory = "http"
  boot_command = [
    "e<down><down><down><end>",
    " autoinstall ds=nocloud;",
    "<F10>",
  ]
  boot_wait = "5s"

  communicator = "ssh"
  ssh_username = "ubuntu"
  ssh_password = var.pkr_ssh_password # default is "ubuntu" as used in http/user-data, make sure to change in both places
  ssh_timeout  = "60m"

  convert_to_template = true
}

source "vsphere-iso" "ubuntu24" {
  vcenter_server      = var.vsphere_server != "" ? var.vsphere_server : " "
  username            = var.vsphere_user != "" ? var.vsphere_user : " "
  password            = var.vsphere_password != "" ? var.vsphere_password : " "
  insecure_connection = true

  datacenter = var.vsphere_datacenter
  cluster    = var.vsphere_cluster != "" ? var.vsphere_cluster : " "
  datastore  = var.vsphere_datastore
  folder     = var.vsphere_folder

  vm_name              = "iso-packer-ubuntu24-${local.timestamp}"
  guest_os_type        = "ubuntu64Guest"
  CPUs                 = 4
  RAM                  = 16384
  disk_controller_type = ["pvscsi"]
  storage {
    disk_size             = 30000
    disk_thin_provisioned = true
  }

  network_adapters {
    network      = var.vsphere_network
    network_card = "vmxnet3"
  }

  boot_order = "disk,cdrom"

  cd_files = [

    "./http/meta-data",

    "./http/user-data"]

  cd_label = "cidata"


  iso_url      = local.iso_url
  iso_checksum = local.iso_checksum

  boot_command = [
    "e<down><down><down><end>",
    " autoinstall ds=nocloud;",
    "<F10>",
  ]

  http_directory = "http"

  communicator = "ssh"
  ssh_username = "ubuntu"
  ssh_password = var.pkr_ssh_password # default is "ubuntu" as used in http/user-data, make sure to change in both places
  ssh_timeout  = "30m"

  convert_to_template = true

}

######################
# Rhel vSphere ISO sources
######################

source "vsphere-iso" "rhel8" {
  vcenter_server      = var.vsphere_server != "" ? var.vsphere_server : " "
  username            = var.vsphere_user != "" ? var.vsphere_user : " "
  password            = var.vsphere_password != "" ? var.vsphere_password : " "
  insecure_connection = true

  datacenter = var.vsphere_datacenter
  cluster    = var.vsphere_cluster != "" ? var.vsphere_cluster : " "
  datastore  = var.vsphere_datastore
  folder     = var.vsphere_folder


  vm_name              = "iso-packer-rhel8-${local.timestamp}"
  guest_os_type        = "rhel8_64Guest"
  CPUs                 = 4
  RAM                  = 16384
  disk_controller_type = ["pvscsi"]
  storage {
    disk_size             = 30000
    disk_thin_provisioned = true
  }

  network_adapters {
    network      = var.vsphere_network
    network_card = "vmxnet3"
  }

  boot_order = "disk,cdrom"

  iso_paths = [
      "[${var.vsphere_datastore}] packer_cache/rhel8_ks.iso",
  ]


  iso_url      = local.iso_url
  iso_checksum = local.iso_checksum

  boot_command = [
    "<enter><enter"
  ]

  communicator = "ssh"
  ssh_username = "builder"
  ssh_password = var.pkr_ssh_password # default is "builder" as used in http/rhel/8/ks.cfg, make sure to change in both places
  ssh_timeout  = "30m"

  convert_to_template = true

}

source "vsphere-iso" "rhel9" {
  vcenter_server      = var.vsphere_server != "" ? var.vsphere_server : " "
  username            = var.vsphere_user != "" ? var.vsphere_user : " "
  password            = var.vsphere_password != "" ? var.vsphere_password : " "
  insecure_connection = true

  datacenter = var.vsphere_datacenter
  cluster    = var.vsphere_cluster != "" ? var.vsphere_cluster : " "
  datastore  = var.vsphere_datastore
  folder     = var.vsphere_folder

  vm_name              = "iso-packer-rhel9-${local.timestamp}"
  guest_os_type        = "rhel9_64Guest"
  CPUs                 = 4
  RAM                  = 16384
  disk_controller_type = ["pvscsi"]
  storage {
    disk_size             = 30000
    disk_thin_provisioned = true
  }

  network_adapters {
    network      = var.vsphere_network
    network_card = "vmxnet3"
  }

  iso_url      = local.iso_url
  iso_checksum = local.iso_checksum

  boot_order = "disk,cdrom"

  iso_paths = [
      "[${var.vsphere_datastore}] packer_cache/rhel9_ks.iso",
  ]

  boot_command = [
    "<enter><enter"
  ]

  communicator = "ssh"
  ssh_username = "builder"
  ssh_password = var.pkr_ssh_password # default is "builder" as used in http/rhel/9/ks.cfg, make sure to change in both places
  ssh_timeout  = "30m"

  convert_to_template = true

}

######################
# Ubuntu Raw/Qcow2 sources
######################

source "qemu" "ubuntu22" {
  vm_name = "qemu-${local.qemu_format}-packer-ubuntu22-${local.timestamp}"

  memory              = 16384
  cpus                = 4
  accelerator         = "none"
  disk_size           = "20G"
  net_device          = "virtio-net"
  disk_interface      = "virtio"
  headless            = true
  use_default_display = true

  format = var.format
  cd_files = [

    "./http/meta-data",

  "./http/user-data"]

  cd_label = "cidata"

  iso_url      = local.iso_url
  iso_checksum = local.iso_checksum

  boot_wait = "5s"
  boot_command = [
    "e<down><down><down><end>",
    " autoinstall ds=nocloud;",
    "<F10>",
  ]

  http_directory = "http"
  communicator   = "ssh"
  ssh_username   = "ubuntu"
  ssh_password   = var.pkr_ssh_password # default is "ubuntu" as used in http/qemu/user-data, make sure to change in both places
  ssh_timeout    = "60m"

  output_directory = "${local.qemu_output_directory}/ubuntu22"
}

source "qemu" "ubuntu24" {
  vm_name = "qemu-${local.qemu_format}-packer-ubuntu24-${local.timestamp}"

  memory              = 16384
  cpus                = 4
  accelerator         = "none"
  disk_size           = "20G"
  net_device          = "virtio-net"
  disk_interface      = "virtio"
  headless            = true
  use_default_display = true

  format = var.format
  cd_files = [

    "./http/meta-data",

  "./http/user-data"]

  cd_label = "cidata"

  iso_url      = local.iso_url
  iso_checksum = local.iso_checksum

  boot_wait = "5s"
  boot_command = [
    "e<down><down><down><end>",
    " autoinstall ds=nocloud;",
    "<F10>",
  ]


  http_directory = "http"
  communicator   = "ssh"
  ssh_username   = "ubuntu"
  ssh_password   = var.pkr_ssh_password # default is "ubuntu" as used in http/qemu/user-data, make sure to change in both places
  ssh_timeout    = "60m"

  output_directory = "${local.qemu_output_directory}/ubuntu24"
}

######################
# Rhel Raw/Qcow2 sources
######################

source "qemu" "rhel8" {
  vm_name          = "qemu-${local.qemu_format}-packer-rhel8-${local.timestamp}"
  accelerator      = "kvm"
  disk_size        = "20000"
  net_device       = "virtio-net"
  disk_interface   = "virtio"
  shutdown_command = "echo 'builder' | sudo -S shutdown -P now"

  headless            = true

  format = var.format

  iso_url      = local.iso_url
  iso_checksum = local.iso_checksum

  boot_wait = "5s"
  boot_command = [
    "<up><tab> text inst.ks=",
    "http://{{ .HTTPIP }}:{{ .HTTPPort }}",
    "/rhel/8/ks.cfg<enter><wait>",
  ]

  qemuargs = [
    ["-m", "2048M"],
    ["-smp", "2"],
    ["-nographic"],
    ["-serial", "stdio"],
    ["-monitor", "none"]
  ]

  http_directory = "http"
  communicator   = "ssh"
  ssh_username   = "builder"
  ssh_password   = var.pkr_ssh_password # default is "builder" as used in http/rhel/8/ks.cfg, make sure to change in both places
  ssh_timeout    = "60m"

  output_directory = "${local.qemu_output_directory}/rhel${local.rhel_os}"
}

source "qemu" "rhel9" {
  vm_name          = "qemu-${local.qemu_format}-packer-rhel9-${local.timestamp}"
  accelerator      = "kvm"
  disk_size        = "20000"
  net_device       = "virtio-net"
  disk_interface   = "virtio"
  shutdown_command = "echo 'builder' | sudo -S shutdown -P now"

  headless            = true

  format = var.format

  iso_url      = local.iso_url
  iso_checksum = local.iso_checksum

  boot_wait = "5s"
  boot_command = [
    "<up><tab> text inst.ks=",
    "http://{{ .HTTPIP }}:{{ .HTTPPort }}",
    "/rhel/9/ks.cfg<enter><wait>",
  ]

  qemuargs = [
    ["-cpu", "host,+nx"],
    ["-m", "2048M"],
    ["-smp", "2"],
    ["-nographic"],
    ["-serial", "stdio"],
    ["-monitor", "none"]
  ]

  http_directory = "http"
  communicator   = "ssh"
  ssh_username   = "builder"
  ssh_password   = var.pkr_ssh_password # default is "builder" as used in http/rhel/9/ks.cfg, make sure to change in both places
  ssh_timeout    = "60m"

  output_directory = "${local.qemu_output_directory}/rhel${local.rhel_os}"
}

######################
# Generalized build for Ubuntu 22.04/24.04 and Rhel 8/9 to install nodeadm
######################
build {
  name = "general-build"
  sources = [
    "source.amazon-ebs.ubuntu22",
    "source.amazon-ebs.ubuntu24",
    "source.amazon-ebs.rhel8",
    "source.amazon-ebs.rhel9",
    "source.vsphere-iso.ubuntu22",
    "source.vsphere-iso.ubuntu24",
    "source.vsphere-iso.rhel8",
    "source.vsphere-iso.rhel9",
    "source.qemu.ubuntu22",
    "source.qemu.ubuntu24",
    "source.qemu.rhel8",
    "source.qemu.rhel9"
  ]


  provisioner "shell" {
    script = "./provisioner_ubuntu.sh"
    environment_vars = [
      "nodeadm_link=${local.nodeadm_link}",
      "auth_value=${local.auth_value}",
      "k8s_version=${var.k8s_version}"
    ]

    only = ["amazon-ebs.ubuntu22", "amazon-ebs.ubuntu24", "vsphere-iso.ubuntu22", "vsphere-iso.ubuntu24", "qemu.ubuntu22", "qemu.ubuntu24"]
  }

    provisioner "shell" {
    script = "./provisioner_rhel.sh"
    environment_vars = [
      "rhsm_username=${var.rhsm_username}",
      "rhsm_password=${var.rhsm_password}",
      "nodeadm_link=${local.nodeadm_link}",
      "auth_value=${local.auth_value}",
      "rhel_version=${var.rhel_version}",
      "k8s_version=${var.k8s_version}"
    ]
    only = ["amazon-ebs.rhel8", "amazon-ebs.rhel9","qemu.rhel8", "qemu.rhel9", "vsphere-iso.rhel8", "vsphere-iso.rhel9"]
  }
}