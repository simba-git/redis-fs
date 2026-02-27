from test import TestCase


class PathNormalize(TestCase):
    def getname(self):
        return "Path normalization — dot, dotdot, double slash"

    def test(self):
        r = self.redis
        k = self.test_key

        # Write through a normalized path, read back via messy path.
        r.execute_command("FS.ECHO", k, "/a/b/c.txt", "data")

        # Double slashes.
        assert r.execute_command("FS.CAT", k, "//a//b//c.txt") == b"data"

        # Dot in path.
        assert r.execute_command("FS.CAT", k, "/a/./b/./c.txt") == b"data"

        # Dotdot in path.
        assert r.execute_command("FS.CAT", k, "/a/b/x/../c.txt") == b"data"
        assert r.execute_command("FS.CAT", k, "/a/b/../b/c.txt") == b"data"

        # Trailing slash (should resolve to same dir for dirs).
        r.execute_command("FS.MKDIR", k, "/mydir")
        stat1 = r.execute_command("FS.STAT", k, "/mydir")
        stat2 = r.execute_command("FS.STAT", k, "/mydir/")
        d1 = dict(zip(stat1[0::2], stat1[1::2]))
        d2 = dict(zip(stat2[0::2], stat2[1::2]))
        assert d1[b"type"] == d2[b"type"]

        # Root variations.
        assert r.execute_command("FS.TEST", k, "/") == 1
        assert r.execute_command("FS.TEST", k, "//") == 1
        assert r.execute_command("FS.TEST", k, "/./") == 1
