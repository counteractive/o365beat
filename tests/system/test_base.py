from o365beat import BaseTest

import os


class Test(BaseTest):

    def test_base(self):
        """
        Basic test with exiting O365beat normally
        """
        self.render_config_template(
            path=os.path.abspath(self.working_dir) + "/log/*"
        )

        o365beat_proc = self.start_beat()
        self.wait_until(lambda: self.log_contains("o365beat is running"))
        exit_code = o365beat_proc.kill_and_wait()
        assert exit_code == 0
