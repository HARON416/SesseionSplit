import { mount } from 'svelte';
import App from './App.svelte';
import './app.css';
import { applyTheme, readTheme } from './theme';

applyTheme(readTheme());

export default mount(App, { target: document.getElementById('app')! });
