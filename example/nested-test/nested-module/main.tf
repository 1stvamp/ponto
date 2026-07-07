module "remote_module" {
  source = "git::https://github.com/1stvamp/ponto.git//example/random-test/random-name"

  max_length = "3"

}