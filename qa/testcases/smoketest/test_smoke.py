import config
import json
import requests


createIndex = { 'options': { 'keys': False } }
createField = { 'options': { 'type': 'int', 'min': 0, 'max': 100000 } }
headers = {"Authorization": "Bearer " + config.admin_token}

def setup_module(module):
    data_to_send = json.dumps(createIndex).encode("utf-8")
    response = requests.post("https://" + config.datanode0 + ":10101/index/user", data = data_to_send, verify = False, headers = headers)
    assert response.status_code == 200
    assert response.headers["Content-Type"] == "application/json"
    resp_body = response.json()
    assert resp_body['success'] == True

    data_to_send = json.dumps(createField).encode("utf-8")
    response = requests.post("https://" + config.datanode0 + ":10101/index/user/field/stats", data = data_to_send, verify = False, headers = headers)
    assert response.status_code == 200
    assert response.headers["Content-Type"] == "application/json"
    resp_body = response.json()
    assert resp_body['success'] == True


def teardown_module(module):
    response = requests.delete("https://" + config.datanode0 + ":10101/index/user", verify = False, headers = headers)
    assert response.status_code == 200
    assert response.headers["Content-Type"] == "application/json"
    resp_body = response.json()
    assert resp_body['success'] == True


def test_api_is_responding():
    response = requests.get("https://" + config.datanode0 + ":10101/status", verify = False, headers = headers)
    assert response.status_code == 200
    assert response.headers["Content-Type"] == "application/json"
    resp_body = response.json()
    assert resp_body['state'] == "NORMAL"
    

def test_sql3_is_responding():
    response = requests.post("https://" + config.datanode0 + ":10101/sql", data = "select 1", verify = False, headers = headers)
    assert response.status_code == 200
    assert response.headers["Content-Type"] == "application/json"
    resp_body = response.json()
    assert resp_body['schema']['fields'][0]['type'] == "INT"
    assert resp_body['data'][0][0] == 1

def test_get_index_api():
    response = requests.get("https://" + config.datanode0 + ":10101/index/user", verify = False, headers = headers)
    assert response.status_code == 200
    assert response.headers["Content-Type"] == "application/json"
    resp_body = response.json()
    assert resp_body['name'] == "user"
    
    
def test_set_and_read_query_api():
    response = requests.post("https://" + config.datanode0 + ":10101/index/user/query", data = "Set(10, stats=1)", verify = False, headers = headers)
    assert response.status_code == 200
    assert response.headers["Content-Type"] == "application/json"
    resp_body = response.json()
    assert resp_body['results'][0] == True

    response = requests.post("https://" + config.datanode0 + ":10101/index/user/query", data = "Row(stats=1)", verify = False, headers = headers)
    assert response.status_code == 200
    assert response.headers["Content-Type"] == "application/json"
    resp_body = response.json()
    assert resp_body['results'][0]['columns'][0] == 10