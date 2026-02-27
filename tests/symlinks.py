from test import TestCase


class Symlinks(TestCase):
    def getname(self):
        return "FS.LN and FS.READLINK — symbolic links"

    def test(self):
        r = self.redis
        k = self.test_key

        # Create a file, then a symlink to it.
        r.execute_command("FS.ECHO", k, "/target.txt", "hello")
        assert r.execute_command("FS.LN", k, "/target.txt", "/link") == b"OK"

        # READLINK returns the target string.
        assert r.execute_command("FS.READLINK", k, "/link") == b"/target.txt"

        # CAT follows symlinks.
        assert r.execute_command("FS.CAT", k, "/link") == b"hello"

        # Symlink to a directory.
        r.execute_command("FS.MKDIR", k, "/realdir")
        r.execute_command("FS.ECHO", k, "/realdir/f.txt", "inside")
        r.execute_command("FS.LN", k, "/realdir", "/dirlink")
        listing = r.execute_command("FS.LS", k, "/dirlink")
        assert b"f.txt" in listing

        # Cannot create link at root.
        try:
            r.execute_command("FS.LN", k, "/target.txt", "/")
            assert False, "Expected error creating link at root"
        except Exception:
            pass

        # Cannot overwrite existing path.
        try:
            r.execute_command("FS.LN", k, "/target.txt", "/link")
            assert False, "Expected error on existing link"
        except Exception:
            pass

        # READLINK on non-symlink fails.
        try:
            r.execute_command("FS.READLINK", k, "/target.txt")
            assert False, "Expected error readlink on file"
        except Exception:
            pass

        # Relative symlink target.
        r.execute_command("FS.LN", k, "target.txt", "/rellink")
        assert r.execute_command("FS.READLINK", k, "/rellink") == b"target.txt"
        # CAT should resolve relative to parent of link.
        assert r.execute_command("FS.CAT", k, "/rellink") == b"hello"

        # Chain of symlinks.
        r.execute_command("FS.LN", k, "/link", "/chain1")
        r.execute_command("FS.LN", k, "/chain1", "/chain2")
        assert r.execute_command("FS.CAT", k, "/chain2") == b"hello"

        # Dangling symlink (target doesn't exist).
        r.execute_command("FS.LN", k, "/nonexistent", "/dangling")
        assert r.execute_command("FS.CAT", k, "/dangling") is None
