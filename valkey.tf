variable "valkey_password" {
  type        = string
  default     = "strimzi-valkey-test"
  sensitive   = true
  description = "Password for Yandex Managed Redis; use the same in redis.password for Producer/Consumer"
}

resource "yandex_mdb_redis_cluster" "valkey" {
  name        = "strimzi-valkey"
  folder_id   = data.yandex_client_config.client.folder_id
  network_id  = yandex_vpc_network.strimzi.id
  environment = "PRODUCTION"
  tls_enabled = false

  config {
    password          = var.valkey_password
    maxmemory_policy  = "ALLKEYS_LRU"
    version           = "9.0-valkey"
  }

  resources {
    resource_preset_id = "hm3-c2-m8"
    disk_type_id      = "network-ssd"
    disk_size         = 65
  }

  host {
    zone      = "ru-central1-a"
    subnet_id = yandex_vpc_subnet.strimzi-a.id
  }
}

output "external_valkey" {
  value = {
    host     = "c-${yandex_mdb_redis_cluster.valkey.id}.rw.mdb.yandexcloud.net"
    port     = 6379
    password = var.valkey_password
  }
  sensitive = true
}

output "valkey_address" {
  value       = "c-${yandex_mdb_redis_cluster.valkey.id}.rw.mdb.yandexcloud.net:6379"
  description = "Use as REDIS_ADDR (plain TCP, no TLS)"
}
