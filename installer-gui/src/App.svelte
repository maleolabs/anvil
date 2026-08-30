<script>
  import { onMount } from 'svelte'
  import { invoke } from '@tauri-apps/api/core'
  import { open } from '@tauri-apps/plugin-dialog'

  let formsData = {}
  let formsOrder = []
  let values = {}
  let errors = {}
  let showErrorDialog = false
  let errorField = ''
  let errorMsg = ''
  let installDir = ''
  let running = false
  let complete = false

  onMount(async () => {
    try {
      const raw = await invoke('get_forms_json')
      if (raw) formsData = typeof raw === 'string' ? JSON.parse(raw) : raw
    } catch {}
    // Fallback: try to load from window __FORMS_JSON__ embedded or fetch forms.json
    if (Object.keys(formsData).length === 0) {
      try {
        const res = await fetch('/forms.json')
        if (res.ok) formsData = await res.json()
      } catch {}
    }
    if (Object.keys(formsData).length === 0 && typeof window !== 'undefined' && window.__FORMS_JSON__) {
      formsData = window.__FORMS_JSON__
    }
    formsOrder = Object.keys(formsData)
    for (const fname of formsOrder) {
      values[fname] = {}
      errors[fname] = {}
      for (const f of (formsData[fname].fields || [])) values[fname][f.name] = ''
    }
  })

  function isVisible(formName, field) {
    if (!field.when) return true
    const v = values[formName]?.[field.when.field]
    return v === field.when.value
  }

  function validateField(formName, field, val) {
    const all = values[formName] || {}
    if (field.required && (!val || val.trim() === '')) return `${field.name} is required`
    if (field.minLength != null && val && val.length < field.minLength) return `${field.name} must be at least ${field.minLength} characters`
    if (field.type === 'email' && val) {
      if (!/^[^@\s]+@[^@\s]+\.[^@\s]+$/.test(val)) return `${field.name} must be a valid email`
    }
    if (field.type === 'number' && val) {
      if (!/^-?[0-9]+(\.[0-9]+)?$/.test(val)) return `${field.name} must be a valid number`
    }
    if (field.pattern && val) {
      try { if (!new RegExp(field.pattern).test(val)) return `${field.name} does not match pattern` } catch {}
    }
    if (field.confirmation && val) {
      if (val !== all[field.confirmation]) return `${field.name} does not match ${field.confirmation}`
    }
    if (field.type === 'select' && val && field.options?.length) {
      if (!field.options.includes(val)) return `${field.name} must be one of [${field.options.join(', ')}]`
    }
    return ''
  }

  function onInput(formName, field, e) {
    values[formName][field.name] = e.target.value
    if (!isVisible(formName, field)) { errors[formName][field.name] = ''; return }
    const msg = validateField(formName, field, e.target.value)
    errors[formName][field.name] = msg
    if (msg) { errorField = field.name; errorMsg = msg; showErrorDialog = false }
  }

  function collectPayload() {
    const payload = { forms: {} }
    for (const fname of formsOrder) {
      payload.forms[fname] = {}
      for (const f of (formsData[fname].fields || [])) {
        if (!isVisible(fname, f)) continue
        payload.forms[fname][f.name] = values[fname][f.name] ?? ''
      }
    }
    return payload
  }

  async function choosePath() {
    const selected = await open({ directory: true, multiple: false })
    if (typeof selected === 'string') installDir = selected
  }

  async function onSubmit() {
    if (!installDir) { errorField = 'install path'; errorMsg = 'Choose an install directory'; showErrorDialog = true; return }
    // Full validation before collect
    let firstErr = null
    for (const fname of formsOrder) {
      for (const f of (formsData[fname].fields || [])) {
        if (!isVisible(fname, f)) continue
        const msg = validateField(fname, f, values[fname][f.name] ?? '')
        errors[fname][f.name] = msg
        if (msg && !firstErr) firstErr = { field: f.name, msg }
      }
    }
    if (firstErr) {
      errorField = firstErr.field
      errorMsg = firstErr.msg
      showErrorDialog = true
      return
    }
    const payload = collectPayload()
    running = true
    try {
      await invoke('collect_forms', { forms: payload.forms })
      await invoke('verify_before_extract')
      await invoke('extract_payload', { destDir: installDir })
      await invoke('apply_setup', { forms: payload.forms, destDir: installDir })
      complete = true
    } catch (e) {
      errorField = 'collect_forms'
      errorMsg = String(e)
      showErrorDialog = true
    } finally { running = false
    }
  }
</script>

<main>
  {#if !complete}
    <div class="install-path">
      <span class="label">Install location</span>
      <button type="button" on:click={choosePath}>Choose folder</button>
      {#if installDir}<code>{installDir}</code>{/if}
    </div>
  {/if}
  {#each formsOrder as formName}
    <section>
      <h2>{formsData[formName].title || formName}</h2>
      {#each (formsData[formName].fields || []) as field}
        {#if isVisible(formName, field)}
          <div class="field">
            <span class="label">{field.label || field.name}{field.required ? ' *' : ''}</span>
            {#if field.type === 'text'}
              <input type="text" value={values[formName]?.[field.name] || ''} on:input={(e)=>onInput(formName, field, e)} placeholder={field.label || field.name} />
            {:else if field.type === 'email'}
              <input type="email" value={values[formName]?.[field.name] || ''} on:input={(e)=>onInput(formName, field, e)} placeholder="email@example.com" />
            {:else if field.type === 'password'}
              <input type="password" value={values[formName]?.[field.name] || ''} on:input={(e)=>onInput(formName, field, e)} />
              {#if field.confirmation}
                <small>confirmation: must match {field.confirmation}</small>
              {/if}
            {:else if field.type === 'select'}
              <select value={values[formName]?.[field.name] || ''} on:change={(e)=>onInput(formName, field, e)}>
                <option value="">-- select --</option>
                {#each field.options as opt}<option value={opt}>{opt}</option>{/each}
              </select>
            {:else if field.type === 'number'}
              <input type="number" value={values[formName]?.[field.name] || ''} on:input={(e)=>onInput(formName, field, e)} />
            {:else if field.type === 'textarea'}
              <textarea value={values[formName]?.[field.name] || ''} on:input={(e)=>onInput(formName, field, e)} rows="4"></textarea>
            {/if}
            {#if errors[formName]?.[field.name]}
              <span class="error">{errors[formName][field.name]}</span>
            {/if}
          </div>
        {/if}
      {/each}
    </section>
  {/each}
  {#if complete}
    <p class="success">Installation complete.</p>
  {:else}
    <button on:click={onSubmit} disabled={running || !formsOrder.length}>{running ? 'Installing…' : 'Install'}</button>
  {/if}
  {#if showErrorDialog}
    <dialog open>
      <p><strong>{errorField}:</strong> {errorMsg}</p>
      <button on:click={()=>showErrorDialog=false}>OK</button>
    </dialog>
  {/if}
</main>

<style>
  main { font-family: system-ui; padding: 2rem; max-width: 600px; margin: auto; }
  .field { margin: 1rem 0; display: flex; flex-direction: column; }
  .label { font-weight: 600; margin-bottom: 0.25rem; }
  .error { color: #c00; font-size: 0.85rem; margin-top: 0.25rem; }
  dialog { border: 1px solid #ccc; padding: 1rem; }
  section { margin-bottom: 2rem; border-bottom: 1px solid #eee; padding-bottom: 1rem; }
</style>
