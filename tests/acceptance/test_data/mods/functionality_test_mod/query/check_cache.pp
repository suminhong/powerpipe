query "check_cache" {
  sql = <<-EOQ
    select 
        case
            when mod(id,2)=0 then 'alarm'
            when mod(id,2)=1 then 'ok'
        end status,
        unique_col as resource,
        id as reason
    from chaos.chaos_cache_check where id=2
  EOQ
}
