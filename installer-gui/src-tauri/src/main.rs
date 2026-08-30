#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

use flate2::read::GzDecoder;
use serde_json::Value;
use sha2::{Digest, Sha256};
use std::env;
use std::fs;
use std::path::{Component, Path, PathBuf};
use tar::Archive;
use tauri::Manager;

const ARTIFACT: &str = "resources/installer-artifact.tar.gz";
const ARTIFACT_SHA256: &str = "resources/installer-artifact.sha256";
const FORMS: &str = "resources/forms.json";
const SETUP: &str = "resources/setup.json";

#[tauri::command]
fn get_app_metadata(app: tauri::AppHandle) -> Result<Value, String> {
    let path = resource_path(&app, "resources/app.json")?;
    if !path.exists() {
        return Ok(serde_json::json!({"name": "Anvil"}));
    }
    serde_json::from_slice(&fs::read(path).map_err(|e| e.to_string())?).map_err(|e| e.to_string())
}

fn resource_path(app: &tauri::AppHandle, name: &str) -> Result<PathBuf, String> {
    app.path()
        .resolve(name, tauri::path::BaseDirectory::Resource)
        .map_err(|e| format!("resolve resource {name}: {e}"))
}

fn has_gui() -> bool {
    if cfg!(target_os = "windows") {
        return true;
    }
    if env::var("ANVIL_FORCE_TUI").as_deref() == Ok("1") {
        return false;
    }
    if env::var("ANVIL_FORCE_GUI").as_deref() == Ok("1") {
        return true;
    }
    env::var("DISPLAY").is_ok() || env::var("WAYLAND_DISPLAY").is_ok()
}

fn safe_destination(root: &Path, entry: &Path) -> Result<PathBuf, String> {
    if entry.is_absolute() {
        return Err("absolute archive path rejected".into());
    }
    let mut clean = PathBuf::new();
    for component in entry.components() {
        match component {
            Component::Normal(part) => clean.push(part),
            Component::CurDir => {}
            Component::ParentDir => return Err("path traversal rejected".into()),
            _ => return Err("unsupported archive path rejected".into()),
        }
    }
    if clean.as_os_str().is_empty() {
        return Err("empty archive path rejected".into());
    }
    Ok(root.join(clean))
}

fn sha256_file(path: &Path) -> Result<String, String> {
    let data = fs::read(path).map_err(|e| format!("read artifact: {e}"))?;
    let mut hasher = Sha256::new();
    hasher.update(data);
    Ok(format!("{:x}", hasher.finalize()))
}

#[tauri::command]
fn get_forms_json(app: tauri::AppHandle) -> Result<Value, String> {
    let path = resource_path(&app, FORMS)?;
    if !path.exists() {
        return Ok(Value::Object(Default::default()));
    }
    serde_json::from_slice(&fs::read(path).map_err(|e| e.to_string())?).map_err(|e| e.to_string())
}

#[tauri::command]
fn choose_install_dir(path: String) -> Result<String, String> {
    let path = PathBuf::from(path);
    if !path.is_absolute() {
        return Err("install path must be absolute".into());
    }
    fs::create_dir_all(&path).map_err(|e| format!("create install path: {e}"))?;
    Ok(path.to_string_lossy().into_owned())
}

#[tauri::command]
fn collect_forms(forms: Value) -> Result<String, String> {
    let output =
        env::var("INSTALLER_FORMS_JSON").unwrap_or_else(|_| "/tmp/installer-forms.json".into());
    fs::write(
        &output,
        serde_json::to_vec_pretty(&forms).map_err(|e| e.to_string())?,
    )
    .map_err(|e| e.to_string())?;
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        fs::set_permissions(&output, fs::Permissions::from_mode(0o600))
            .map_err(|e| e.to_string())?;
    }
    eprintln!("[installer-gui] forms collected (redacted) gui=true");
    Ok(output)
}

#[tauri::command]
fn verify_before_extract(app: tauri::AppHandle) -> Result<bool, String> {
    let artifact = resource_path(&app, ARTIFACT)?;
    if !artifact.exists() {
        return Err("embedded artifact missing — verification failed".into());
    }
    let actual = sha256_file(&artifact)?;
    let expected = fs::read_to_string(resource_path(&app, ARTIFACT_SHA256)?)
        .map_err(|e| format!("read artifact checksum: {e}"))?
        .trim()
        .to_owned();
    if expected.is_empty() || actual != expected {
        return Err("artifact checksum mismatch — verification failed closed".into());
    }
    Ok(true)
}

#[tauri::command]
fn extract_payload(app: tauri::AppHandle, dest_dir: String) -> Result<String, String> {
    verify_before_extract(app.clone())?;
    let root = PathBuf::from(dest_dir);
    if !root.is_absolute() {
        return Err("install path must be absolute".into());
    }
    fs::create_dir_all(&root).map_err(|e| e.to_string())?;
    let file = fs::File::open(resource_path(&app, ARTIFACT)?).map_err(|e| e.to_string())?;
    let mut archive = Archive::new(GzDecoder::new(file));
    for item in archive.entries().map_err(|e| e.to_string())? {
        let mut entry = item.map_err(|e| e.to_string())?;
        let target = safe_destination(&root, &entry.path().map_err(|e| e.to_string())?)?;
        if entry.header().entry_type().is_dir() {
            fs::create_dir_all(&target).map_err(|e| e.to_string())?;
        } else {
            if let Some(parent) = target.parent() {
                fs::create_dir_all(parent).map_err(|e| e.to_string())?;
            }
            entry.unpack(&target).map_err(|e| e.to_string())?;
        }
    }
    Ok(root.to_string_lossy().into_owned())
}

#[tauri::command]
fn get_setup_json(app: tauri::AppHandle) -> Result<Value, String> {
    let path = resource_path(&app, SETUP)?;
    if !path.exists() {
        return Ok(Value::Object(Default::default()));
    }
    serde_json::from_slice(&fs::read(path).map_err(|e| e.to_string())?).map_err(|e| e.to_string())
}

#[tauri::command]
fn apply_setup(app: tauri::AppHandle, forms: Value, dest_dir: String) -> Result<String, String> {
    let setup = get_setup_json(app)?;
    let env_map = setup
        .get("env_map")
        .and_then(Value::as_object)
        .cloned()
        .unwrap_or_default();
    let root = PathBuf::from(dest_dir);
    if !root.is_absolute() {
        return Err("install path must be absolute".into());
    }
    let env_path = root.join(
        setup
            .get("env_file")
            .and_then(Value::as_str)
            .unwrap_or(".env"),
    );
    let mut output = if env_path.exists() {
        fs::read_to_string(&env_path).map_err(|e| e.to_string())?
    } else {
        String::new()
    };
    for (key, template) in env_map {
        let mut value = template.as_str().unwrap_or_default().to_string();
        if let Some(inner) = value
            .strip_prefix("{{forms.")
            .and_then(|v| v.strip_suffix("}}"))
        {
            let mut parts = inner.splitn(2, '.');
            let form = parts.next().unwrap_or_default();
            let field = parts.next().unwrap_or_default();
            value = forms
                .get(form)
                .and_then(|v| v.get(field))
                .and_then(Value::as_str)
                .unwrap_or_default()
                .to_string();
        }
        let prefix = format!("{key}=");
        let line = format!("{key}={value}");
        let mut lines: Vec<String> = output.lines().map(str::to_owned).collect();
        if let Some(pos) = lines.iter().position(|line| line.starts_with(&prefix)) {
            lines[pos] = line;
        } else {
            lines.push(line);
        }
        output = lines.join("\n") + "\n";
    }
    if let Some(parent) = env_path.parent() {
        fs::create_dir_all(parent).map_err(|e| e.to_string())?;
    }
    fs::write(&env_path, output).map_err(|e| e.to_string())?;
    eprintln!("[installer-gui] setup applied (redacted) env_keys_only");
    Ok(env_path.to_string_lossy().into_owned())
}

fn main() {
    if !has_gui() {
        eprintln!("[installer-gui] GUI unavailable; use the legacy TUI installer");
        std::process::exit(2);
    }
    tauri::Builder::default()
        .plugin(tauri_plugin_dialog::init())
        .invoke_handler(tauri::generate_handler![
            get_forms_json,
            get_app_metadata,
            get_setup_json,
            choose_install_dir,
            collect_forms,
            verify_before_extract,
            extract_payload,
            apply_setup
        ])
        .run(tauri::generate_context!())
        .expect("error while running native installer GUI");
}
