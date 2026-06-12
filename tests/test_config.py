"""Unit tests for read_config — the non-exiting loader used by reload (R)."""

from matterbase.app import read_config


def test_valid_config(tmp_path):
    cfg = tmp_path / "c.yml"
    cfg.write_text(f"notes_dir: {tmp_path}\neditor: vi\n", encoding="utf-8")
    config, err = read_config(str(cfg))
    assert err == ""
    assert config["notes_dir"] == str(tmp_path)
    assert config["_config_path"] == str(cfg)


def test_missing_file(tmp_path):
    config, err = read_config(str(tmp_path / "nope.yml"))
    assert config is None
    assert "not found" in err


def test_invalid_yaml(tmp_path):
    cfg = tmp_path / "c.yml"
    cfg.write_text("notes_dir: [unclosed\n", encoding="utf-8")
    config, err = read_config(str(cfg))
    assert config is None
    assert "YAML" in err


def test_not_a_mapping(tmp_path):
    cfg = tmp_path / "c.yml"
    cfg.write_text("- just\n- a list\n", encoding="utf-8")
    config, err = read_config(str(cfg))
    assert config is None


def test_missing_notes_dir_key(tmp_path):
    cfg = tmp_path / "c.yml"
    cfg.write_text("editor: vi\n", encoding="utf-8")
    config, err = read_config(str(cfg))
    assert config is None
    assert "notes_dir" in err


def test_nonexistent_notes_dir(tmp_path):
    cfg = tmp_path / "c.yml"
    cfg.write_text(f"notes_dir: {tmp_path}/missing\n", encoding="utf-8")
    config, err = read_config(str(cfg))
    assert config is None
    assert "not exist" in err
