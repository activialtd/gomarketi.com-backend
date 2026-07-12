# Used only by infrastructure/terraform/bootstrap (the state backend itself,
# which by definition can't be created via the backend it creates).
region            = "eu-west-1"
aws_profile       = "gomarketi-terraform"
state_bucket_name = "gomarketi-terraform-state-336617737576"
lock_table_name   = "gomarketi-terraform-lock"
