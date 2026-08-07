Vagrant.configure("2") do |config|
  config.vm.box = "cloud-image/ubuntu-24.04"
  config.vm.hostname = "dev"

  config.vm.provider "libvirt" do |lv|
    lv.memory = 8192
    lv.cpus = 4
    lv.machine_virtual_size = 80
  end

  config.vm.provision "ansible" do |a|
    a.playbook = "playbook.yml"
    a.galaxy_role_file = "requirements.yml"
  end
end
