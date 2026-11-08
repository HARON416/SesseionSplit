"""The baseline pipeline must stop on failure and preserve active work."""
from pathlib import Path
import tempfile
import unittest
from unittest import mock

import baseline_pipeline as pipeline
from workspace_lock import exclusive_lock


class PipelineTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.base = Path(self.temp.name)
        patch = mock.patch.object(pipeline.bootstrap, 'BASE', self.base)
        patch.start()
        self.addCleanup(patch.stop)

    def test_stage_failure_prevents_build_and_verification(self):
        children = [mock.Mock(), mock.Mock()]
        children[0].wait.return_value = 0
        children[1].wait.return_value = 1
        with mock.patch.object(pipeline.subprocess, 'Popen', side_effect=children) as start:
            with self.assertRaisesRegex(RuntimeError, 'hooks failed'):
                pipeline.run_pipeline('sync', 2)
        self.assertEqual([call.args[0][-3] for call in start.call_args_list],
                         ['sync', 'hooks'])
        state = pipeline.json.loads((self.base / 'baseline-pipeline.json').read_text())
        self.assertEqual(state['status'], 'failed')
        self.assertEqual(state['stage'], 'hooks')

    def test_busy_pipeline_does_not_overwrite_its_state(self):
        state = self.base / 'baseline-pipeline.json'
        state.write_text('{"status":"running","stage":"build"}')
        before = state.read_bytes()
        with exclusive_lock(self.base / 'baseline-pipeline.lock'):
            with self.assertRaisesRegex(RuntimeError, 'Workspace is busy'):
                pipeline.run_pipeline('sync', 2)
        self.assertEqual(state.read_bytes(), before)

    def test_active_direct_stage_prevents_pipeline_start(self):
        with exclusive_lock(self.base / 'bootstrap.lock'):
            with mock.patch.object(pipeline.subprocess, 'Popen') as start:
                with self.assertRaisesRegex(RuntimeError, 'bootstrap stage is active'):
                    pipeline.run_pipeline('sync', 2)
                start.assert_not_called()

    def test_resume_runs_only_remaining_stages(self):
        child = mock.Mock()
        child.wait.return_value = 0
        with mock.patch.object(pipeline.subprocess, 'Popen', return_value=child) as start:
            pipeline.run_pipeline('build', 2)
        self.assertEqual([call.args[0][-3] for call in start.call_args_list],
                         ['build', 'verify'])
        state = pipeline.json.loads((self.base / 'baseline-pipeline.json').read_text())
        self.assertEqual(state['status'], 'passed')

    def test_interruption_stops_child_before_releasing_pipeline(self):
        child = mock.Mock()
        child.wait.side_effect = [KeyboardInterrupt(), 1]
        child.poll.return_value = None
        with mock.patch.object(pipeline.subprocess, 'Popen', return_value=child):
            with self.assertRaises(KeyboardInterrupt):
                pipeline.run_pipeline('sync', 2)
        child.terminate.assert_called_once()
        self.assertEqual(child.wait.call_count, 2)
        state = pipeline.json.loads((self.base / 'baseline-pipeline.json').read_text())
        self.assertEqual(state['status'], 'interrupted')
