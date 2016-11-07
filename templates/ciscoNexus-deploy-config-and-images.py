import os
import shutil
import sys
import urllib2
from time import sleep

# Python module names vary depending on nxos version
try:
    from cli import cli
except:
    from cisco import cli

tmp_config_path = "volatile:poap.cfg"
tmp_config_path_unix = "/volatile/poap.cfg"

class CiscoDeployExcept(Exception):
    def __init__(self, message=None):
        super(CiscoDeployExcept, self).__init__(message)

def deploy_startup_config():
    startup_config_uri = '<%= (hasOwnProperty("startupConfigUri") ? startupConfigUri : "" )%>'
    if not startup_config_uri:
        return

    try:
        poap_log("Removing {0}".format(tmp_config_path_unix))
        os.remove(tmp_config_path_unix)
    except:
        poap_log("Removing {0} failed".format(tmp_config_path_unix))

    poap_log("Downloading script at {}".format(startup_config_uri))
    config = urllib2.urlopen(startup_config_uri).read()
    poap_log("{0}".format(config))
    with open(tmp_config_path_unix, 'w') as tmp_config:
        tmp_config.write(config)
    poap_log("Copying {0} to startup-config".format(tmp_config_path))
    cli("copy %s startup-config" % tmp_config_path)
    poap_log("deploy_startup_config finished")

log_filename = "/bootflash/poap.log"

# setup log file and associated utils
poap_log_file = open(log_filename, "a+")

def poap_log (info):
    poap_log_file.write("ciscoNexus-deploy-config-and-images.py:")
    poap_log_file.write(info)
    poap_log_file.write("\n")
    poap_log_file.flush()
    print "poap_py_log:" + info
    sys.stdout.flush()

def poap_log_close ():
    poap_log_file.close()

def main():
    try:
        deploy_startup_config()
        # deploy_boot_images()
    except Exception as e:
        poap_log("Deploy startup or boot failed")
        poap_log_close()
        # Don't swallow exceptions otherwise the Cisco switch will think the POAP was a success
        # and proceed to boot rather than retrying
        raise e

    # Copying to scheduled-config is necessary for POAP to exit on the next
    # reboot and apply the configuration. We want to merge the running-config
    # changes made by both the startup-config deployment
    # and the boot image deployment.
    # The issue is if we copy to scheduled-config MORE THAN ONCE it will
    # trigger POAP/config application MORE THAN ONCE as well, which we don't want.
    # So we have to do all these operations in the same script, that way they
    # are not order-dependant.
    try:
        poap_log("Removing {0}".format(tmp_config_path_unix))
        os.remove(tmp_config_path_unix)
    except:
        poap_log("Removing {0} failed".format(tmp_config_path_unix))

    #poap_log("From: %s to %s" % ("running-config", "startup-config"))
    #cli("copy running-config startup-config")
    poap_log("From: %s to %s" % ("startup-config", tmp_config_path))
    cli("copy startup-config %s" % tmp_config_path)
    poap_log("From: %s to %s" % (tmp_config_path, "scheduled-config"))
    cli("configure terminal ; copy %s scheduled-config" % tmp_config_path)

    poap_log_close()
