"""Declarative custom catalog overlays; no Docker or database required."""
import copy
from pathlib import Path
import tempfile
import unittest
import catalogs


class CatalogTests(unittest.TestCase):
    def setUp(self):
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        self.root = Path(temp.name).resolve()
        self.user = self.root / 'custom'
        self.write(self.user / 'npc/scripts.conf', '// empty')
        self.write(self.user / 'db/item_db.yml', 'Header: {Type: ITEM_DB, Version: 3}')
        self.write(self.user / 'db/mob_db.yml', 'Header: {Type: MOB_DB, Version: 4}')

    def write(self, path, text):
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(text)
        return path

    def test_override_and_additions_follow_load_list_not_directory_scan(self):
        server = self.root / 'server'
        self.write(server / 'conf/script_athena.conf', '// none')
        self.write(server / 'npc/re/scripts_main.conf', 'npc: npc/a.txt\n')
        self.write(server / 'npc/a.txt', 'prontera,1,2,4\tscript\tOld\t100,{\n}\n')
        old = {'id': 'prontera:1:2:Old', 'map': 'prontera', 'source': {'path': 'npc/a.txt'},
               'sprite_key': '100', 'sprite_id': 100, 'display_sprite_id': 100, 'image_available': True}
        base = {'entries': [old], 'sources': {}, 'version': 'baseline'}
        self.write(self.user / 'npc/overrides/a.txt', '// disabled')
        self.write(self.user / 'npc/additions/a.txt', 'prontera,4,5,4\tscript\tNew\t100,{\n}\n')
        self.write(self.user / 'npc/additions/not_loaded.txt', 'prontera,8,9,4\tscript\tHidden\t100,{\n}\n')
        self.write(self.user / 'npc/scripts.conf', 'npc: additions/a.txt\n')
        result = catalogs.npc_catalog(base, server, self.user / 'npc')
        self.assertEqual([e['name'] for e in result['entries']], ['New'])
        self.assertTrue(result['entries'][0]['game_visible'])
        self.assertFalse(result['entries'][0]['capabilities']['can_teleport_to_npc'])
        (self.user / 'npc/overrides/a.txt').unlink()
        self.assertEqual(len(catalogs.npc_catalog(base, server, self.user / 'npc')['entries']), 2)

    def test_nested_custom_import_and_cycle(self):
        server = self.root / 'server'
        self.write(server / 'conf/script_athena.conf', '')
        self.write(server / 'npc/re/scripts_main.conf', '')
        self.write(self.user / 'npc/scripts.conf', 'import: additions/event.conf')
        self.write(self.user / 'npc/additions/event.conf', 'npc: additions/event.txt')
        self.write(self.user / 'npc/additions/event.txt', '// event')
        self.assertEqual(len(catalogs.npc_sources(server, self.user / 'npc')), 1)
        self.write(self.user / 'npc/additions/event.conf', 'import: additions/event.conf')
        with self.assertRaisesRegex(ValueError, 'Circular'): catalogs.npc_sources(server, self.user / 'npc')

    def test_database_overlay_keeps_omitted_fields_and_reverts_from_base(self):
        base = {'items': {'501': {'AegisName': 'Red_Potion', 'names': {'zh-CN': '红色药水'}, 'Buy': 10, 'Sell': 5, 'Weight': 70}}}
        original = copy.deepcopy(base)
        self.write(self.user / 'db/item_db.yml', 'Header: {Type: ITEM_DB, Version: 3}\nBody:\n- Id: 501\n  Buy: 20\n  Name: 定制药水\n')
        result = catalogs.database_catalog(base, self.user / 'db', 'item_db.yml', 'ITEM_DB', 'items')
        self.assertEqual(result['items']['501']['Weight'], 70)
        self.assertEqual(result['items']['501']['Sell'], 10)
        self.assertEqual(base, original)
        self.write(self.user / 'db/item_db.yml', 'Header: {Type: ITEM_DB, Version: 3}\n')
        self.assertEqual(catalogs.database_catalog(base, self.user / 'db', 'item_db.yml', 'ITEM_DB', 'items')['items'], base['items'])

    def test_database_drop_index_replaces_and_unindexed_appends(self):
        base = {'AegisName': 'Poring', 'names': {'zh-CN': '波利'}, 'Drops': [{'Item': 'Apple', 'Rate': 100}]}
        result = catalogs.merge_record(base, {'Drops': [{'Index': 0, 'Item': 'Apple', 'Rate': 500}, {'Item': 'Red_Potion', 'Rate': 200}]}, 'MOB_DB')
        self.assertEqual([d['Rate'] for d in result['Drops']], [500, 200])
        with self.assertRaises(ValueError): catalogs.merge_record(base, {'Drops': [{'Index': 8, 'Item': 'Apple', 'Rate': 1}]}, 'MOB_DB')

    def test_database_import_modes_and_clear(self):
        base = {'items': {'1': {'AegisName': 'Old', 'names': {'zh-CN': 'Old'}}}}
        self.write(self.user / 'db/item_db.yml', 'Header: {Type: ITEM_DB, Version: 3, Clear: true}\nFooter:\n  Imports:\n  - Path: db/import/new.yml\n    Mode: Renewal\n  - Path: db/import/missing.yml\n    Mode: Prerenewal\n')
        self.write(self.user / 'db/new.yml', 'Header: {Type: ITEM_DB, Version: 3}\nBody:\n- Id: 2\n  AegisName: New\n  Name: New\n')
        result = catalogs.database_catalog(base, self.user / 'db', 'item_db.yml', 'ITEM_DB', 'items')
        self.assertEqual(list(result['items']), ['2'])
        self.write(self.user / 'db/new.yml', 'Header: {Type: ITEM_DB, Version: 3}\nFooter:\n  Imports:\n  - Path: db/import/item_db.yml\n')
        with self.assertRaisesRegex(ValueError, 'Circular'): catalogs.database_catalog(base, self.user / 'db', 'item_db.yml', 'ITEM_DB', 'items')


if __name__ == "__main__":
    unittest.main()
