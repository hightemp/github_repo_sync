#!/bin/bash

# Проверяем что скрипт запущен с правами root
if [ "$EUID" -ne 0 ]; then 
    echo "Please run as root"
    exit 1
fi

# Конфигурационные переменные
APP_NAME="github_repo_sync"
APP_DESCRIPTION="GitHub Repository Sync Service"
APP_PATH="/usr/local/bin/${APP_NAME}"
CONFIG_PATH="/etc/${APP_NAME}/config.yaml"
SERVICE_PATH="/etc/systemd/system/${APP_NAME}.service"
USER="github_repo_sync"
GROUP="github_repo_sync"

# Проверяем наличие исполняемого файла
if [ ! -f "./${APP_NAME}" ]; then
    echo "Error: executable file not found!"
    exit 1
fi

# Создаем пользователя и группу если они не существуют
if ! getent group "$GROUP" >/dev/null; then
    groupadd "$GROUP"
fi

if ! getent passwd "$USER" >/dev/null; then
    useradd -r -g "$GROUP" -s /bin/false "$USER"
fi

# Создаем директории
mkdir -p "/etc/${APP_NAME}"
mkdir -p "/var/log/${APP_NAME}"
mkdir -p "$(dirname $APP_PATH)"

# Копируем исполняемый файл
cp "./${APP_NAME}" "$APP_PATH"
chmod 755 "$APP_PATH"

# Копируем конфиг если он существует
if [ -f "./config.yaml" ]; then
    cp "./config.yaml" "$CONFIG_PATH"
    chmod 640 "$CONFIG_PATH"
    chown "${USER}:${GROUP}" "$CONFIG_PATH"
else
    echo "Warning: config.yaml not found in current directory"
fi

# Создаем systemd service файл
cat > "$SERVICE_PATH" << EOF
[Unit]
Description=${APP_DESCRIPTION}
After=network.target

[Service]
Type=simple
User=${USER}
Group=${GROUP}
ExecStart=${APP_PATH} -config ${CONFIG_PATH}
Restart=always
RestartSec=10
StandardOutput=append:/var/log/${APP_NAME}/${APP_NAME}.log
StandardError=append:/var/log/${APP_NAME}/${APP_NAME}.log

[Install]
WantedBy=multi-user.target
EOF

# Устанавливаем права на лог-файлы
chown -R "${USER}:${GROUP}" "/var/log/${APP_NAME}"
chmod 755 "/var/log/${APP_NAME}"

# Перезагружаем systemd и включаем сервис
systemctl stop "${APP_NAME}.service"
systemctl daemon-reload
systemctl enable "${APP_NAME}.service"
systemctl start "${APP_NAME}.service"

echo "Installation completed!"
echo "To start the service run: systemctl start ${APP_NAME}"
echo "To check status run: systemctl status ${APP_NAME}"
echo "To view logs run: journalctl -u ${APP_NAME}"

# Выводим предупреждение если конфиг не был найден
if [ ! -f "$CONFIG_PATH" ]; then
    echo ""
    echo "WARNING: You need to create and configure ${CONFIG_PATH} before starting the service"
fi
