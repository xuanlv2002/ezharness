import { mount } from 'svelte'
import '../app.css'
import BrowserApp from './BrowserApp.svelte'

const app = mount(BrowserApp, { target: document.getElementById('app')! })

export default app
