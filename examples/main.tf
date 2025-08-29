resource "core-v1_transaction" "trans-interfaces" {
  crs         = local.interfaces
  description = "Terraform transaction"
  dry_run     = false
}
