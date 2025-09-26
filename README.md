# PalMon - A Palworld Server Monitor & Admin Tool

PalMon is a lightweight, standalone, low-overhead monitoring and administration tool for your Palworld dedicated server. It is written in Go and provides a simple web-based dashboard accessible from any device. It runs as a single executable with zero dependencies, making it easy to set up and manage.

## Features

- **Automatic Health Monitoring:** Periodically checks the server's REST API to ensure it is running and responsive.
- **Auto-Restart on Crash/Freeze:** If the server becomes unresponsive for a configurable number of checks, PalMon will automatically terminate the hung process and restart it.
- **Web-Based Dashboard:** A clean, modern, read-only UI for anyone to view the server's status.
  - Live Server FPS & Uptime.
  - Current vs. Max Player Count.
  - A list of all currently online players and their levels.
- **Secure Admin Panel:**
  - Login with a username and password to access admin tools.
  - **Announce:** Send in-game messages (broadcasts) to all players.
  - **Kick/Ban:** Kick or ban players directly from the UI.
  - **Graceful Shutdown & Restart:** Safely shut down or restart the server, allowing it to save first.
  - **Manual Refresh:** Force an immediate health check to get the latest data.
- **Discord Notifications:** Optional integration to send a message to a Discord channel when the server restarts.

## Setup & Installation

Follow these steps to get PalMon running.

### Prerequisites

- A working Palworld Dedicated Server.
- The `PalMon.exe`, `config.json`, and the `templates` folder in the same directory.

### Step 1: Configure Your Palworld Server

PalMon relies on the server's built-in REST API. You must enable it.

1.  Stop your Palworld server.
2.  Open your server's configuration file, located at:
    `.../PalServer/Pal/Saved/Config/WindowsServer/PalWorldSettings.ini`
3.  Add or edit the following lines in the `[/Script/Pal.PalGameWorldSettings]` section:

    ```ini
    RESTAPIEnabled=True
    RESTAPIPort=8212
    AdminPassword="YourSecretPasswordHere"
    ```
    - The `AdminPassword` is used for **both** RCON and the REST API. **Do not use special characters like `@` or `"` in this password.**

4.  Save the file and restart your Palworld server.

### Step 2: Configure PalMon

Open the `config.json` file in a text editor and fill in the details. **This is the most important step.**

- Set the `path` to your PalServer directory.
- Set the `password` in the `rcon` section to match the `AdminPassword` from `PalWorldSettings.ini`.
- Set a secure `username` and `password` for the `web` admin panel.

(See the detailed configuration breakdown below).

### Step 3: Configure Network & Firewall

For others to see your dashboard, you need to allow traffic on the port PalMon uses (default is `7777`).

1.  **Windows Firewall:** Create a new **Inbound Rule** to **Allow** connections for **TCP Port `7777`**.
2.  **Router (for Internet access):** Log in to your router and create a **Port Forwarding** rule to forward **TCP Port `7777`** to the local IP address of your server machine.

### Step 4: Run PalMon

Simply double-click `PalMon.exe`. A command prompt window will open and show the application's logs. You can minimize this window, but it must remain running for the monitor to work.

## Usage

- **Public Dashboard:** `http://<Your-Server-IP>:7777`
- **Admin Login:** Click the "Login to Admin" link in the footer. Your browser will prompt you for the `username` and `password` you set in the `web` section of your `config.json`.

## Configuration (`config.json`)

| Section   | Key                  | Description                                                                                             |
| :-------- | :------------------- | :------------------------------------------------------------------------------------------------------ |
| `server`  | `path`               | The full path to your `PalServer` directory (e.g., `G:\\SteamLibrary\\steamapps\\common\\PalServer`).      |
|           | `executable`         | The name of the server executable file (usually `PalServer.exe`).                                       |
| `monitor` | `startup_delay_seconds` | How long to wait after starting the server before the first health check. `120` (2 minutes) is safe.     |
|           | `health_check_interval_seconds` | How often to check the server's health after startup. `60` (1 minute) is a good balance.         |
|           | `unhealthy_threshold`| Number of consecutive failed checks before triggering a restart. `3` is a safe default.                |
| `rcon`    | `password`           | **Crucial.** Must match the `AdminPassword` in `PalWorldSettings.ini`. Used for REST API authentication. |
| `rest_api`| `port`               | The port for the server's REST API. Default is `8212`.                                                  |
| `web`     | `listen_address`     | The IP and port for the PalMon web dashboard to run on. `0.0.0.0:7777` makes it accessible on all network interfaces. |
|           | `page_title`         | A fallback title for the web page if the server name can't be fetched.                                  |
|           | `username`           | The username for the admin panel login.                                                                 |
|           | `password`           | The password for the admin panel login. **Choose something strong!**                                    |
| `discord` | `webhook_url`        | Your Discord webhook URL. Leave as `""` (empty string) to disable notifications.                        |

## How to Compile (from source)

1.  Install the latest version of Go.
2.  Place all `.go` files, `config.json`, and the `templates` folder in a project directory.
3.  Open a terminal in the project directory.
4.  Run `go mod tidy` to clean dependencies.
5.  Run `go build -ldflags="-s -w" -o PalMon.exe .` to create the executable.