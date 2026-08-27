output "network_name" {
  value = docker_network.minimal.name
}

output "postgres_volume_name" {
  value = docker_volume.postgres_data.name
}

output "gitea_volume_name" {
  value = docker_volume.gitea_data.name
}

output "woodpecker_server_volume_name" {
  value = docker_volume.woodpecker_server_data.name
}

output "trivy_db_cache_volume_name" {
  value = docker_volume.trivy_db_cache.name
}
