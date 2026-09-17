"""PFB subnet checks with Docker host/none networks present."""

import subprocess
import unittest
from unittest import mock

from pfb.docker import _assert_subnet_available
from pfb.errors import PFBError


class DockerNetworkTests(unittest.TestCase):
    def check(self, networks):
        with mock.patch("pfb.docker._run", return_value="host none bridge"), \
             mock.patch("pfb.docker._run_json", return_value=networks), \
             mock.patch("pfb.docker.subprocess.run", return_value=subprocess.CompletedProcess(
                 [], 0, stdout="[]", stderr="")):
            _assert_subnet_available("172.29.240.0/24")

    def test_host_and_none_networks_have_no_ipam_subnets(self):
        self.check([
            {"Name": "host", "IPAM": {"Config": None}},
            {"Name": "none", "IPAM": {"Config": []}},
            {"Name": "bridge", "IPAM": {"Config": [{"Subnet": "172.17.0.0/16"}]}},
        ])

    def test_real_bridge_overlap_is_still_rejected(self):
        with self.assertRaisesRegex(PFBError, "PFB_NETWORK_SUBNET_CONFLICT"):
            self.check([
                {"Name": "host", "IPAM": {"Config": None}},
                {"Name": "bridge", "IPAM": {"Config": [{"Subnet": "172.29.0.0/16"}]}},
            ])
