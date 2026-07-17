# Notification Orchestrator

Servicio Go que acepta notificaciones para email, SMS y push, y las entrega de forma asíncrona mediante adaptadores de proveedor. Está diseñado como una primera versión mantenible: el transporte HTTP, la orquestación, las validaciones, los proveedores y la persistencia tienen contratos separados.

## Ejecutar

Requiere Go 1.22 o superior. No usa dependencias de terceros.

```bash
go test ./...
go run ./cmd/server
```

El servidor escucha en `:8080`. Los proveedores locales escriben una entrega exitosa en stdout; no envían mensajes reales.

```bash
curl -i -X POST http://localhost:8080/v1/notifications \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: welcome-user-42' \
  -d '{"recipients":["ana@example.com","bob@example.com"],"channel":"email","message":"Bienvenidos"}'

curl http://localhost:8080/v1/notifications/<notification-id>
curl 'http://localhost:8080/v1/notifications?limit=20'
```

## API

`POST /v1/notifications` acepta:

```json
{
  "recipients": ["ana@example.com", "bob@example.com"],
  "channel": "email",
  "message": "Bienvenidos"
}
```

Devuelve `202 Accepted` con el recurso de notificación. El encabezado opcional `Idempotency-Key` permite reintentos seguros: la misma clave con el mismo cuerpo devuelve el recurso existente (`200`); con un cuerpo distinto devuelve `409`.

`GET /v1/notifications/{id}` devuelve el estado agregado y el de cada destinatario. Los estados agregados son `queued`, `processing`, `delivered`, `partially_failed` y `failed`.

`GET /v1/notifications?limit=20&cursor=<id>` lista en orden descendente de creación. `limit` está entre 1 y 100. La respuesta contiene `next_cursor` cuando hay otra página.

Errores de entrada devuelven `400`; una cola saturada devuelve `503` y `Retry-After: 1` para aplicar backpressure sin crear una notificación que no se puede procesar.

## Configuración

Todas las variables son opcionales:

| Variable | Predeterminado | Propósito |
| --- | ---: | --- |
| `HTTP_ADDR` | `:8080` | Dirección HTTP |
| `NOTIFICATION_QUEUE_CAPACITY` | `256` | Máximo de solicitudes pendientes |
| `NOTIFICATION_WORKERS` | `4` | Notificaciones procesadas a la vez |
| `RECIPIENT_WORKERS` | `8` | Destinatarios a la vez por notificación |
| `VALIDATION_WORKERS` | `4` | Validaciones independientes en paralelo |
| `MAX_RECIPIENTS` | `100` | Límite por solicitud |
| `MAX_MESSAGE_BYTES` | `10000` | Tamaño máximo del mensaje |
| `MAX_DELIVERY_ATTEMPTS` | `3` | Intentos por destinatario |
| `PROVIDER_ATTEMPT_TIMEOUT` | `3s` | Timeout por llamada a proveedor |
| `RETRY_BASE_DELAY` | `100ms` | Backoff exponencial inicial |

## Decisiones de diseño

- **Asincronía y límites.** La petición se guarda y se encola antes de responder. Una cola con capacidad fija y pools de workers acotan memoria, goroutines y llamadas externas durante ráfagas. La presión se comunica como `503`, en lugar de aceptar trabajo que una instancia efímera no podrá conservar.
- **Estado por destinatario.** Una solicitud con varios receptores crea varias entregas. El estado de la notificación se deriva de ellas, de modo que una entrega fallida no oculta las exitosas.
- **Validación con cancelación temprana.** Las validaciones son interfaces independientes y se ejecutan con paralelismo acotado. El primer fallo cancela el contexto compartido; los validadores de I/O deben respetarlo. Las validaciones estructurales baratas se realizan antes de persistir.
- **Adaptadores extensibles.** `provider.Provider` es la única dependencia del orquestador respecto a email/SMS/push. Agregar un canal requiere un adaptador y registrarlo, sin cambiar la lógica de reintentos.
- **Confiabilidad frente a proveedores.** Cada llamada tiene timeout. Los errores transitorios se reintentan con backoff exponencial; `provider.Permanent(err)` termina de inmediato. El diseño es *at-least-once*: un timeout puede dejar ambiguo si el proveedor aceptó el mensaje, por lo que los adaptadores reales deben propagar una clave de idempotencia al proveedor.
- **Idempotencia de entrada.** El encabezado evita duplicar solicitudes por reintentos de cliente. Las comparaciones incluyen canal, contenido y destinatarios en su orden original.

## Supuestos y trade-offs

- Esta entrega usa un repositorio en memoria y proveedores de log para poder arrancar sin infraestructura. Por tanto, reiniciar el proceso pierde estado y trabajo pendiente; no es durable aún.
- No se implementa autenticación/autorización: se presupone que un gateway confiable la realiza. En producción se debe validar el principal y limitar que solo consulte sus notificaciones.
- Las preferencias y rate limiting están expuestos como validadores explícitos, con implementación local permisiva. La política exacta no fue especificada.
- La ordenación por cursor es apropiada para el repositorio actual; una implementación SQL usaría un cursor opaco basado en `(created_at, id)` y un índice correspondiente.

## Próximos pasos para producción

1. Sustituir el repositorio por PostgreSQL y un patrón transactional outbox; un worker durable reclamaría trabajos con locks y reanudaría pendientes tras reinicios.
2. Implementar adaptadores reales, secretos gestionados, idempotencia de proveedor y circuit breakers/rate limits por proveedor.
3. Añadir autenticación, cifrado de contenido sensible, auditoría, políticas de retención y redacción de errores de proveedor.
4. Añadir métricas, trazas, logs estructurados, endpoints de readiness y alertas sobre cola, latencia, reintentos y fallos.
5. Definir semántica exacta de preferencias, plantillas, prioridades, programaciones y límites por tenant.
