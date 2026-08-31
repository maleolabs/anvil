<script>
  import { onMount } from 'svelte'
  import { invoke } from '@tauri-apps/api/core'
  import { open } from '@tauri-apps/plugin-dialog'

  let formsData = {}, formsOrder = [], values = {}, errors = {}
  let productName = 'Anvil', productVersion = '', showErrorDialog = false
  let errorField = '', errorMsg = '', installDir = '', running = false, complete = false
  $: fieldCount = formsOrder.reduce((count, name) => count + (formsData[name]?.fields || []).filter((field) => isVisible(name, field)).length, 0)
  $: filledCount = formsOrder.reduce((count, name) => count + (formsData[name]?.fields || []).filter((field) => isVisible(name, field) && values[name]?.[field.name]).length, 0)
  $: progress = complete ? 100 : fieldCount ? Math.round((filledCount / fieldCount) * 100) : 0

  onMount(async () => {
    try {
      const raw = await invoke('get_forms_json')
      formsData = raw ? (typeof raw === 'string' ? JSON.parse(raw) : raw) : {}
    } catch {}
    try {
      const metadata = await invoke('get_app_metadata')
      productName = metadata?.name || productName
      productVersion = metadata?.version || ''
    } catch {}
    formsOrder = Object.keys(formsData)
    for (const fname of formsOrder) {
      values[fname] = {}; errors[fname] = {}
      for (const field of (formsData[fname].fields || [])) values[fname][field.name] = ''
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
    try {
      const selected = await open({ directory: true, multiple: false, title: 'Choose installation folder' })
      if (typeof selected === 'string') installDir = await invoke('choose_install_dir', { path: selected })
    } catch (e) { errorField = 'Install folder'; errorMsg = String(e); showErrorDialog = true }
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
      errorField = 'Installation'
      errorMsg = String(e)
      showErrorDialog = true
    } finally { running = false
    }
  }
</script>

<svelte:head><title>{productName} installer</title></svelte:head>
<main>
  <header class="hero">
    <div class="brand-mark" aria-hidden="true">A</div>
    <div><p class="eyebrow">{productName} setup</p><h1>Install {productName}</h1><p class="lede">Configure your installation, then let the installer handle the rest.</p></div>
    {#if productVersion}<span class="version">v{productVersion}</span>{/if}
  </header>
  <div class="stepper" aria-label="Installation progress">
    <div class:active={!complete} class:done={complete}><span>1</span><strong>Configure</strong></div><div class="line" class:done={complete}></div><div class:active={complete}><span>2</span><strong>Complete</strong></div>
  </div>
  <div class="progress-track" role="progressbar" aria-label="Form completion" aria-valuemin="0" aria-valuemax="100" aria-valuenow={progress}><span style={`width: ${progress}%`}></span></div>
  {#if !complete}
    <div class="install-path" class:invalid={showErrorDialog && !installDir}>
      <div><p class="section-kicker">Required</p><h2>Installation folder</h2><p class="help">Choose where {productName} should be installed.</p></div>
      <button class="secondary" type="button" on:click={choosePath}>Browse folders</button>
      {#if installDir}<code aria-label="Selected installation folder">{installDir}</code>{:else}<p class="placeholder">No folder selected yet</p>{/if}
    </div>
  {/if}
  {#each formsOrder as formName}
    <section class="form-section">
      <div class="section-heading"><p class="section-kicker">Step {formsOrder.indexOf(formName) + 1}</p><h2>{formsData[formName].title || formName}</h2></div>
      {#each (formsData[formName].fields || []) as field}
        {#if isVisible(formName, field)}
          <div class="field" class:invalid={errors[formName]?.[field.name]}>
            <label for={`${formName}-${field.name}`}>{field.label || field.name}{field.required ? ' *' : ''}</label>
            {#if field.type === 'text'}
              <input id={`${formName}-${field.name}`} type="text" value={values[formName]?.[field.name] || ''} on:input={(e)=>onInput(formName, field, e)} aria-invalid={!!errors[formName]?.[field.name]} />
            {:else if field.type === 'email'}
              <input id={`${formName}-${field.name}`} type="email" value={values[formName]?.[field.name] || ''} on:input={(e)=>onInput(formName, field, e)} placeholder="email@example.com" aria-invalid={!!errors[formName]?.[field.name]} />
            {:else if field.type === 'password'}
              <input id={`${formName}-${field.name}`} type="password" value={values[formName]?.[field.name] || ''} on:input={(e)=>onInput(formName, field, e)} aria-invalid={!!errors[formName]?.[field.name]} />
              {#if field.confirmation}
                <small>confirmation: must match {field.confirmation}</small>
              {/if}
            {:else if field.type === 'select'}
              <select id={`${formName}-${field.name}`} value={values[formName]?.[field.name] || ''} on:change={(e)=>onInput(formName, field, e)} aria-invalid={!!errors[formName]?.[field.name]}>
                <option value="">-- select --</option>
                {#each field.options as opt}<option value={opt}>{opt}</option>{/each}
              </select>
            {:else if field.type === 'number'}
              <input id={`${formName}-${field.name}`} type="number" value={values[formName]?.[field.name] || ''} on:input={(e)=>onInput(formName, field, e)} aria-invalid={!!errors[formName]?.[field.name]} />
            {:else if field.type === 'textarea'}
              <textarea id={`${formName}-${field.name}`} value={values[formName]?.[field.name] || ''} on:input={(e)=>onInput(formName, field, e)} rows="4" aria-invalid={!!errors[formName]?.[field.name]}></textarea>
            {/if}
            {#if errors[formName]?.[field.name]}
               <span class="error" role="alert">{errors[formName][field.name]}</span>
            {/if}
          </div>
        {/if}
      {/each}
    </section>
  {/each}
  {#if complete}
     <div class="success" role="status"><span aria-hidden="true">✓</span><div><h2>Installation complete</h2><p>{productName} is ready to use.</p></div></div>
  {:else}
    <button class="primary" on:click={onSubmit} disabled={running || !formsOrder.length}>{running ? 'Installing…' : `Install ${productName}`}</button>
  {/if}
  {#if showErrorDialog}
    <dialog open>
      <p><strong>{errorField}:</strong> {errorMsg}</p>
       <button class="primary" on:click={()=>showErrorDialog=false}>OK</button>
    </dialog>
  {/if}
</main>

<style>
  :global(*) { box-sizing: border-box } :global(body) { margin: 0; background: #f5f7fa; color: #16212b; font: 14px/1.5 Inter, system-ui, sans-serif } main { max-width: 680px; margin: auto; padding: 34px 28px 26px } .hero { display: flex; gap: 16px; align-items: flex-start; margin-bottom: 30px } .brand-mark { background: #123b53; color: #d9f06a; border-radius: 12px; width: 44px; height: 44px; display: grid; place-items: center; font-weight: 800; font-size: 22px } .eyebrow,.section-kicker { color: #66808e; font-size: 11px; font-weight: 800; letter-spacing: .11em; text-transform: uppercase; margin: 0 0 3px } h1,h2,p { margin-top: 0 } h1 { font-size: 27px; letter-spacing: -.03em; margin-bottom: 4px } h2 { font-size: 17px; margin-bottom: 3px } .lede,.help,.placeholder { color: #6b7c87; margin-bottom: 0 } .version { margin-left: auto; color: #66808e; font-size: 12px } .stepper { display: flex; align-items: center; margin-bottom: 8px } .stepper div:not(.line) { display: flex; align-items: center; gap: 8px; color: #8a99a3; font-size: 12px } .stepper div.active { color: #123b53 } .stepper span { border: 1px solid #c8d2d8; border-radius: 50%; width: 24px; height: 24px; display: grid; place-items: center; font-weight: 700 } .stepper .active span,.stepper .done span { background: #123b53; border-color: #123b53; color: white } .line { height: 1px; background: #d6dfe3; flex: 1; margin: 0 12px } .progress-track { background: #dfe6e9; border-radius: 99px; height: 4px; margin-bottom: 24px; overflow: hidden } .progress-track span { background: #b4cf31; display: block; height: 100%; transition: width .2s ease } .install-path,.form-section { background: white; border: 1px solid #dce4e8; border-radius: 12px; padding: 20px; margin-bottom: 16px; box-shadow: 0 4px 16px #18384b0b } .install-path { display: grid; grid-template-columns: 1fr auto; gap: 6px 18px; align-items: center } .install-path code { grid-column: 1/-1; background: #f1f5f6; border-radius: 6px; padding: 10px 12px; overflow-wrap: anywhere; color: #355766 } .install-path .placeholder { grid-column: 1/-1; font-size: 13px } .section-heading { border-bottom: 1px solid #e8edef; padding-bottom: 13px; margin-bottom: 3px } .field { margin-top: 17px } label { display: block; font-weight: 700; margin-bottom: 6px } input,select,textarea { border: 1px solid #c8d4da; border-radius: 6px; width: 100%; padding: 10px 12px; color: inherit; background: #fff; font: inherit; transition: border .15s, box-shadow .15s } textarea { resize: vertical } input:focus,select:focus,textarea:focus,button:focus-visible { outline: 3px solid #b4cf3155; outline-offset: 2px; border-color: #779100 } .invalid input,.invalid select,.invalid textarea { border-color: #c64747 } .error { display: block; color: #b23636; font-size: 12px; margin-top: 5px } small { color: #71838d; margin-top: 4px } button { border: 0; border-radius: 7px; cursor: pointer; font: inherit; font-weight: 750; padding: 10px 15px } button:disabled { cursor: wait; opacity: .65 } .primary { background: #123b53; color: white; width: 100%; margin-top: 5px } .secondary { background: #edf3f4; color: #123b53 } .success { background: #edf6d6; border: 1px solid #d2e7a2; border-radius: 12px; display: flex; gap: 14px; padding: 20px; align-items: flex-start } .success > span { background: #719100; color: white; border-radius: 50%; width: 26px; height: 26px; display: grid; place-items: center; font-weight: 800 } .success h2 { margin: 0 0 3px } .success p { margin: 0; color: #4b6420 } dialog { border: 1px solid #c8d4da; border-radius: 12px; padding: 22px; max-width: 360px; box-shadow: 0 14px 40px #102b3a33 } dialog::backdrop { background: #102b3a66 } dialog p { margin-bottom: 18px } footer { color: #8a99a3; font-size: 11px; text-align: center; padding-top: 18px } footer strong { color: #5f7079 } @media (max-width: 520px) { main { padding: 25px 16px } .install-path { grid-template-columns: 1fr } .secondary { width: 100% } }
</style>
<footer>Powered by <strong>Anvil</strong> · Maleolabs</footer>
